package transport

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/bilbilmyc/fileupload/web"
)

// Router HTTP 路由器，组装全部路由
type Router struct {
	mux        *http.ServeMux
	middleware *Middleware
	tus        *TusHandler
	rest       *RESTHandler
	download   *DownloadHandler
	batch      *BatchHandler
	uploadSvc  *domain.UploadService
	scanner    Scanner
	health     HealthChecker
}

// Scanner 一致性巡检接口
type Scanner interface {
	Scan(ctx context.Context) (any, error)
}

// HealthChecker 健康检查接口
type HealthChecker interface {
	Check(ctx context.Context) map[string]any
}

// NewRouter 创建路由器并注册所有路由
func NewRouter(mw *Middleware, tus *TusHandler, rest *RESTHandler, download *DownloadHandler, batch *BatchHandler, uploadSvc *domain.UploadService, scanner Scanner, health HealthChecker) *Router {
	r := &Router{
		mux:        http.NewServeMux(),
		middleware: mw,
		tus:        tus,
		rest:       rest,
		download:   download,
		batch:      batch,
		uploadSvc:  uploadSvc,
		scanner:    scanner,
		health:     health,
	}
	r.registerRoutes()
	return r
}

// Handler 返回经过中间件包装的最终 handler
func (r *Router) Handler() http.Handler {
	var h http.Handler = r.mux
	h = r.middleware.Namespace(h)
	h = r.middleware.Auth(h)    // X-Auth-Token 认证
	h = r.middleware.RateLimit(h)
	h = r.middleware.Logging(h) // 请求日志（状态码 + 耗时）
	h = r.middleware.RequestID(h)
	h = r.middleware.Recover(h)
	return h
}

// registerRoutes 注册所有路由
func (r *Router) registerRoutes() {
	// === tus 协议 ===
	r.mux.HandleFunc("POST /uploads", r.tus.CreateUpload)
	r.mux.HandleFunc("HEAD /uploads/{id}", r.tus.GetUploadInfo)
	r.mux.HandleFunc("PATCH /uploads/{id}", r.tus.AppendChunk)
	r.mux.HandleFunc("DELETE /uploads/{id}", r.tus.CancelUpload)

	// === REST 上传 ===
	r.mux.HandleFunc("POST /v1/uploads/init", r.rest.InitUpload)
	r.mux.HandleFunc("GET /v1/uploads/{id}/status", r.rest.GetUploadStatus)
	r.mux.HandleFunc("POST /v1/uploads/{id}/finalize", r.rest.FinalizeUpload)
	r.mux.HandleFunc("PUT /v1/uploads/{id}/chunks/{index}", r.rest.UploadChunk)

	// === 下载 ===
	r.mux.HandleFunc("GET /v1/files/{id}", r.download.GetFile)
	r.mux.HandleFunc("GET /v1/dirs/{id}", r.download.GetDir)

	// === 目录管理 ===
	r.mux.HandleFunc("POST /v1/dirs", r.rest.SubmitDir)
	r.mux.HandleFunc("DELETE /v1/files/{id}", r.rest.DeleteFile)
	r.mux.HandleFunc("DELETE /v1/dirs/{id}", r.rest.DeleteFile)
	r.mux.HandleFunc("GET /v1/ls", r.rest.ListDir)
	r.mux.HandleFunc("GET /v1/stat/{id}", r.rest.StatFile)

	// === 秒传预检 ===
	r.mux.HandleFunc("HEAD /v1/files", r.handleCheckExists)

	// === 批量操作 ===
	r.mux.HandleFunc("POST /v1/batch/delete", r.batch.BatchDelete)
	r.mux.HandleFunc("POST /v1/batch/download", r.batch.BatchDownload)
	r.mux.HandleFunc("POST /v1/batch/move", r.batch.BatchMove)
	r.mux.HandleFunc("POST /v1/batch/copy", r.batch.BatchCopy)
	r.mux.HandleFunc("POST /v1/batch/tags", r.batch.BatchTags)

	// === 管理 ===
	r.mux.HandleFunc("POST /v1/admin/scan", r.handleAdminScan)

	// === 前端 React 构建产物 ===
	staticFS, err := fs.Sub(web.DistFiles, "dist")
	if err == nil {
		r.mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
	}

	// === 健康检查 ===
	r.mux.HandleFunc("GET /health", func(w http.ResponseWriter, req *http.Request) {
		result := map[string]any{"status": "ok"}
		if r.health != nil {
			checks := r.health.Check(req.Context())
			for k, v := range checks {
				result[k] = v
			}
			for _, v := range checks {
				if m, ok := v.(map[string]any); ok {
					if s, _ := m["status"].(string); s != "" && s != "ok" {
						result["status"] = "degraded"
						break
					}
				}
			}
		}
		respondJSON(w, http.StatusOK, result)
	})
}

// handleCheckExists 秒传预检
func (r *Router) handleCheckExists(w http.ResponseWriter, req *http.Request) {
	sha256 := req.URL.Query().Get("sha256")
	if sha256 == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(req.Context())
	name := req.URL.Query().Get("name")

	result, err := r.uploadSvc.CheckExists(req.Context(), sha256, namespace, name)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	if result == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"file_id": result.FileID,
		"sha256":  result.SHA256,
		"size":    result.Size,
	})
}

// handleAdminScan 触发一致性巡检
func (r *Router) handleAdminScan(w http.ResponseWriter, req *http.Request) {
	if r.scanner == nil {
		respondError(w, http.StatusNotImplemented, domain.ErrInvalidArgument)
		return
	}

	report, err := r.scanner.Scan(req.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	respondJSON(w, http.StatusOK, report)
}
