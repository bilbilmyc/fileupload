package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/bilbilmyc/fileupload/internal/lifecycle"
	"github.com/bilbilmyc/fileupload/internal/transport"
)

// Deps 服务端依赖装配。所有字段为已构造好的端口实例。
// 通过 Deps 注入让 Build 函数可在测试中替换任一端口为 fake。
type Deps struct {
	Storage    domain.Storage  // 本地/远端文件存储
	TempFS     domain.Storage  // 临时分片目录（与 Storage 实现可能相同）
	Metadata   domain.Metadata // 热+冷元数据门面
	Compressor domain.Compressor
	Hasher     domain.Hasher
	WorkerPool domain.WorkerPool
	Auth       domain.AuthService // 可为 nil（未启用 JWT）

	// 配置
	UploadCfg   domain.UploadConfig
	DownloadCfg domain.DownloadConfig
	AuthCfg     transport.AuthConfig
	CORSOrigins []string
	ServerCfg   ServerConfig

	// 后台任务
	ReaperInterval time.Duration
	TrashRetention time.Duration
}

// ServerConfig HTTP 服务端配置。
type ServerConfig struct {
	Addr           string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	DebugEndpoints bool
	MetricsToken   string
}

// Server 已装配的服务端，含 HTTP handler 与后台任务句柄。
type Server struct {
	httpServer   *http.Server
	reaper       *lifecycle.SessionReaper
	scanner      *lifecycle.ConsistencyScanner
	trashJanitor *lifecycle.TrashJanitor
}

// HTTP 返回底层 *http.Server（用于 graceful shutdown）。
func (s *Server) HTTP() *http.Server { return s.httpServer }

// Reaper 返回 reaper 句柄（用于 Stop）。
func (s *Server) Reaper() *lifecycle.SessionReaper { return s.reaper }

// Scanner 返回 scanner 句柄（用于手动触发巡检）。
func (s *Server) Scanner() *lifecycle.ConsistencyScanner { return s.scanner }

func (s *Server) TrashJanitor() *lifecycle.TrashJanitor { return s.trashJanitor }

// Build 装配所有依赖并返回可启动的 Server。
// main() 调用此函数后只需 ListenAndServe + 等待信号。
func Build(d Deps) (*Server, error) {
	// 领域服务
	uploadSvc := domain.NewUploadService(d.Metadata, d.Storage, d.TempFS, d.Compressor, d.Hasher, d.WorkerPool, d.UploadCfg)
	downloadSvc := domain.NewDownloadService(d.Metadata, d.Storage, d.Compressor, d.Hasher, d.DownloadCfg)
	batchSvc := domain.NewBatchService(uploadSvc, uploadSvc, downloadSvc, d.Metadata)
	shareSvc := domain.NewShareService(d.Metadata)

	// 传输层
	mw := transport.NewMiddleware().WithCORS(d.CORSOrigins).WithAuth(d.AuthCfg).
		WithObservability(d.ServerCfg.DebugEndpoints, d.ServerCfg.MetricsToken)
	if d.Auth != nil {
		mw.WithJWT(d.Auth)
	}
	go mw.RateLimiterCleanup(10*time.Minute, 5*time.Minute)

	tusHandler := transport.NewTusHandler(uploadSvc)
	restHandler := transport.NewRESTHandler(uploadSvc, downloadSvc).WithNamespaceQuota(d.UploadCfg.NamespaceQuotaBytes)
	downloadHandler := transport.NewDownloadHandler(downloadSvc).WithAuditLogger(d.Metadata)
	batchHandler := transport.NewBatchHandler(batchSvc)
	var authHandler *transport.AuthHandler
	if d.Auth != nil {
		authHandler = transport.NewAuthHandler(d.Auth)
	}
	adminHandler := transport.NewAdminHandler(d.Metadata, d.WorkerPool, d.ServerCfg.Addr, d.ServerCfg.Addr, "", "")
	shareHandler := transport.NewShareHandler(shareSvc, downloadSvc).WithAuditLogger(d.Metadata)
	wsHub := transport.NewWSHub()

	// 后台任务
	reaper := lifecycle.NewSessionReaperWithStorage(d.Metadata, d.TempFS, d.ReaperInterval)
	scanner := lifecycle.NewConsistencyScanner(d.Metadata, d.Storage, d.UploadCfg.DataDir, d.UploadCfg.DataDir)
	trashJanitor := lifecycle.NewTrashJanitor(d.Metadata, uploadSvc, d.TrashRetention, d.ReaperInterval)

	// 健康检查器（不依赖 redis/os，直接走 Storage + Metadata 端口）
	healthChecker := &serverHealth{storage: d.Storage, metadata: d.Metadata}

	router := transport.NewRouter(mw, tusHandler, restHandler, downloadHandler, batchHandler, authHandler, adminHandler, shareHandler, wsHub, uploadSvc, scanner, healthChecker)

	httpServer := &http.Server{
		Addr:         d.ServerCfg.Addr,
		Handler:      router.Handler(),
		ReadTimeout:  d.ServerCfg.ReadTimeout,
		WriteTimeout: d.ServerCfg.WriteTimeout,
		IdleTimeout:  d.ServerCfg.IdleTimeout,
	}

	return &Server{
		httpServer:   httpServer,
		reaper:       reaper,
		scanner:      scanner,
		trashJanitor: trashJanitor,
	}, nil
}

// serverHealth 实现 transport.HealthChecker。
// 通过 Storage / Metadata 端口的 HealthCheck 方法探活，
// 不再直接持有 *redis.Client 或调用 os.* 文件系统 API。
type serverHealth struct {
	storage  domain.Storage
	metadata domain.Metadata
}

func (h *serverHealth) Check(ctx context.Context) map[string]any {
	result := make(map[string]any)

	storageHealth := map[string]any{"status": "ok"}
	if err := h.storage.HealthCheck(ctx); err != nil {
		storageHealth["status"] = "error"
		storageHealth["error"] = err.Error()
	}
	result["storage"] = storageHealth

	metadataHealth := map[string]any{"status": "ok"}
	if err := h.metadata.HealthCheck(ctx); err != nil {
		metadataHealth["status"] = "error"
		metadataHealth["error"] = err.Error()
	}
	result["metadata"] = metadataHealth

	return result
}

// 格式化错误辅助（供 main 使用）
func must(err error, msg string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return nil
}
