package transport

import (
	"context"
	"expvar"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/bilbilmyc/fileupload/internal/metrics"
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
	auth       *AuthHandler
	admin      *AdminHandler
	share      *ShareHandler
	wsHub      *WSHub
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
func NewRouter(mw *Middleware, tus *TusHandler, rest *RESTHandler, download *DownloadHandler, batch *BatchHandler, auth *AuthHandler, admin *AdminHandler, share *ShareHandler, wsHub *WSHub, uploadSvc *domain.UploadService, scanner Scanner, health HealthChecker) *Router {
	r := &Router{
		mux:        http.NewServeMux(),
		middleware: mw,
		tus:        tus,
		rest:       rest,
		download:   download,
		batch:      batch,
		auth:       auth,
		admin:      admin,
		share:      share,
		wsHub:      wsHub,
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
	h = r.middleware.Namespace(h)   // 命名空间注入（读取已验证的 JWT claims）
	h = r.middleware.JWTValidate(h) // JWT 验证（Bearer token）
	h = r.middleware.Auth(h)        // X-Auth-Token 认证
	h = r.middleware.RateLimit(h)
	h = r.middleware.Logging(h) // 请求日志
	h = r.middleware.RequestID(h)
	h = r.middleware.Recover(h)
	h = r.middleware.CORS(h) // CORS（最外层）
	return h
}

// registerRoutes 注册所有路由
func (r *Router) registerRoutes() {
	// === 鉴权 ===
	if r.auth != nil {
		r.mux.HandleFunc("POST /v1/auth/login", r.auth.Login)
		r.mux.HandleFunc("POST /v1/auth/refresh", r.auth.Refresh)
		r.mux.HandleFunc("GET /v1/auth/me", r.auth.Me)
	}

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
	r.mux.HandleFunc("GET /v1/preview/{id}", r.download.GetPreview)

	// === 目录管理 ===
	r.mux.HandleFunc("POST /v1/dirs", r.rest.SubmitDir)
	r.mux.HandleFunc("PATCH /v1/files/{id}", r.rest.RenameFile)
	r.mux.HandleFunc("DELETE /v1/files/{id}", r.rest.DeleteFile)
	r.mux.HandleFunc("DELETE /v1/dirs/{id}", r.rest.DeleteFile)
	r.mux.HandleFunc("GET /v1/ls", r.rest.ListDir)
	r.mux.HandleFunc("GET /v1/stat/{id}", r.rest.StatFile)
	r.mux.HandleFunc("GET /v1/usage", r.rest.GetNamespaceUsage)
	r.mux.HandleFunc("GET /v1/trash", r.rest.ListTrash)
	r.mux.HandleFunc("POST /v1/trash/{id}/restore", r.rest.RestoreTrash)
	r.mux.HandleFunc("DELETE /v1/trash/{id}", r.rest.PurgeTrash)

	// === 秒传预检 ===
	r.mux.HandleFunc("HEAD /v1/files", r.handleCheckExists)

	// === 批量操作（表驱动调度）===
	r.mux.HandleFunc("POST /v1/batch/{action}", r.batch.BatchHandle)
	r.mux.HandleFunc("GET /v1/batch/download", r.batch.BatchDownloadGet)

	// === 管理 ===
	if r.admin != nil {
		r.mux.Handle("GET /v1/admin/status", r.middleware.RequireRole("admin", http.HandlerFunc(r.admin.Status)))
		r.mux.Handle("GET /v1/admin/audit", r.middleware.RequireRole("admin", http.HandlerFunc(r.admin.AuditLog)))
	}
	r.mux.Handle("POST /v1/admin/scan", r.middleware.RequireRole("admin", http.HandlerFunc(r.handleAdminScan)))

	if r.share != nil {
		r.mux.HandleFunc("POST /v1/share", r.share.CreateShare)
		r.mux.HandleFunc("GET /v1/shares", r.share.ListShares)
		r.mux.HandleFunc("DELETE /v1/shares/{token}", r.share.RevokeShare)
		r.mux.HandleFunc("GET /s/{token}", r.share.AccessShare)
		r.mux.HandleFunc("POST /s/{token}", r.share.SubmitSharePassword)
	}

	// === 前端 React 构建产物 ===
	staticFS, err := fs.Sub(web.DistFiles, "dist")
	if err == nil {
		r.mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
	}

	// === WebSocket ===
	if r.wsHub != nil {
		r.mux.HandleFunc("GET /ws", r.wsHub.HandleWebSocket)
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
			updateHealthMetrics(checks)
		}
		respondJSON(w, http.StatusOK, result)
	})

	// === 调试（pprof + expvar）===
	// 默认不注册，避免在生产环境泄露运行时和内存信息；显式打开时也要求 admin role。
	if r.middleware.DebugEndpointsEnabled() {
		r.mux.Handle("GET /debug/pprof/", r.middleware.RequireRole("admin", http.HandlerFunc(pprof.Index)))
		r.mux.Handle("GET /debug/pprof/cmdline", r.middleware.RequireRole("admin", http.HandlerFunc(pprof.Cmdline)))
		r.mux.Handle("GET /debug/pprof/profile", r.middleware.RequireRole("admin", http.HandlerFunc(pprof.Profile)))
		r.mux.Handle("GET /debug/pprof/symbol", r.middleware.RequireRole("admin", http.HandlerFunc(pprof.Symbol)))
		r.mux.Handle("GET /debug/pprof/trace", r.middleware.RequireRole("admin", http.HandlerFunc(pprof.Trace)))
		r.mux.Handle("GET /debug/pprof/goroutine", r.middleware.RequireRole("admin", pprof.Handler("goroutine")))
		r.mux.Handle("GET /debug/pprof/heap", r.middleware.RequireRole("admin", pprof.Handler("heap")))
		r.mux.Handle("GET /debug/pprof/threadcreate", r.middleware.RequireRole("admin", pprof.Handler("threadcreate")))
		r.mux.Handle("GET /debug/pprof/block", r.middleware.RequireRole("admin", pprof.Handler("block")))
		r.mux.Handle("GET /debug/pprof/mutex", r.middleware.RequireRole("admin", pprof.Handler("mutex")))
		r.mux.Handle("GET /debug/vars", r.middleware.RequireRole("admin", expvar.Handler()))
	}

	// === Prometheus 指标（v0.2.0+） ===
	// 每次抓取指标时同步探测后端，保证 fileupload_health_status 不会因为
	// 没有额外调用 /health 而长期停留在旧值。
	r.mux.Handle("GET /metrics", r.middleware.MetricsAuth(http.HandlerFunc(r.handleMetrics)))
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

	// HEAD 响应没有可供客户端读取的响应体，秒传元数据通过响应头返回。
	w.Header().Set("X-File-ID", result.FileID)
	w.Header().Set("X-File-SHA256", result.SHA256)
	w.Header().Set("X-File-Size", strconv.FormatInt(result.Size, 10))
	w.WriteHeader(http.StatusOK)
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

// handleMetrics 在输出 Prometheus 指标前刷新后端健康状态。
// 健康检查失败不会阻断指标输出，Prometheus 仍可读取进程和失败计数器。
func (r *Router) handleMetrics(w http.ResponseWriter, req *http.Request) {
	if r.health != nil {
		updateHealthMetrics(r.health.Check(req.Context()))
	}
	promhttp.Handler().ServeHTTP(w, req)
}

func updateHealthMetrics(checks map[string]any) {
	for component, value := range checks {
		statusMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		status, _ := statusMap["status"].(string)
		if status == "ok" {
			metrics.HealthStatus.WithLabelValues(component).Set(1)
		} else {
			metrics.HealthStatus.WithLabelValues(component).Set(0)
		}
	}
}
