// Command server 是文件上传下载服务的 HTTP 服务端入口。
//
// 启动方式：
//
//	FILEUPLOAD_REDIS_ADDR=localhost:6379 go run ./cmd/server
//	FILEUPLOAD_DB_PATH=/tmp/fileupload.db go run ./cmd/server
//
// 默认配置：
//	- 监听 :8080
//	- 本地文件系统存储（data/）
//	- SQLite 数据库（fileupload.db）
//	- Redis（localhost:6379）
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bilbilmyc/fileupload/internal/adapters/auth"
	"github.com/bilbilmyc/fileupload/internal/adapters/compressor"
	"github.com/bilbilmyc/fileupload/internal/adapters/hasher"
	"github.com/bilbilmyc/fileupload/internal/adapters/metadata"
	"github.com/bilbilmyc/fileupload/internal/adapters/storage"
	"github.com/bilbilmyc/fileupload/internal/config"
	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/bilbilmyc/fileupload/internal/lifecycle"
	"github.com/bilbilmyc/fileupload/internal/transport"
)

// serverHealth 实现 transport.HealthChecker
type serverHealth struct {
	redis   *redis.Client
	dataDir string
	tempDir string
	dbPath  string
}

func (h *serverHealth) Check(ctx context.Context) map[string]any {
	result := make(map[string]any)

	// Redis 检查
	redisHealth := map[string]any{"status": "ok"}
	if err := h.redis.Ping(ctx).Err(); err != nil {
		redisHealth["status"] = "error"
		redisHealth["error"] = err.Error()
	}
	result["redis"] = redisHealth

	// SQLite 检查（通过检查 db 文件是否存在）
	dbHealth := map[string]any{"status": "ok"}
	if h.dbPath != "" && h.dbPath != ":memory:" {
		if _, err := os.Stat(h.dbPath); os.IsNotExist(err) {
			dbHealth["status"] = "error"
			dbHealth["error"] = "数据库文件不存在"
		}
	}
	result["database"] = dbHealth

	// 磁盘检查（data dir 和 temp dir 可写性）
	diskHealth := map[string]any{"status": "ok"}
	for name, dir := range map[string]string{"data_dir": h.dataDir, "temp_dir": h.tempDir} {
		testFile := filepath.Join(dir, ".healthcheck")
		if err := os.WriteFile(testFile, []byte("ok"), 0644); err != nil {
			diskHealth["status"] = "error"
			diskHealth[name] = "不可写: " + err.Error()
		} else {
			os.Remove(testFile)
			diskHealth[name] = "可写"
		}
	}
	result["disk"] = diskHealth

	return result
}

func main() {
	// 加载配置
	configPath := os.Getenv("FILEUPLOAD_CONFIG")
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// === 基础设施 ===

	// 1. 本地存储
	localFS, err := storage.NewLocalFS(cfg.Storage.DataDir)
	if err != nil {
		log.Fatalf("初始化本地存储: %v", err)
	}

	// 1b. 临时分片存储
	tempFS, err := storage.NewLocalFS(cfg.Storage.TempDir)
	if err != nil {
		log.Fatalf("初始化临时存储: %v", err)
	}

	// 2. Redis 热数据
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	// 测试连接
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("警告: Redis 连接失败 (%v)，热数据功能受限", err)
	}
	redisStore := metadata.NewRedisStore(rdb, cfg.Redis.Prefix)

	// 3. SQLite 冷数据
	sqliteStore, err := metadata.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		log.Fatalf("初始化 SQLite: %v", err)
	}

	// 4. Metadata 门面
	metaFacade := metadata.NewFacade(redisStore, sqliteStore)

	// 5. 压缩器
	compress, err := compressor.NewCompressor()
	if err != nil {
		log.Fatalf("初始化压缩器: %v", err)
	}

	// 6. 哈希器
	hasher := hasher.NewSHA256Hasher()

	// === 领域核心 ===

	workerPool := domain.NewSimpleWorkerPool(cfg.Upload.WorkerPoolSize, cfg.Upload.WorkerQueueSize)
	defer workerPool.Stop()

	uploadCfg := domain.UploadConfig{
		SessionTTL:      cfg.Upload.SessionTTL(),
		DataDir:         cfg.Storage.DataDir,
		DefaultChunkSize: cfg.Upload.DefaultChunkSize,
	}
	uploadSvc := domain.NewUploadService(metaFacade, localFS, tempFS, compress, hasher, workerPool, uploadCfg)

	downloadCfg := domain.DownloadConfig{
		DataDir: cfg.Storage.DataDir,
	}
	downloadSvc := domain.NewDownloadService(metaFacade, localFS, compress, hasher, downloadCfg)

	batchSvc := domain.NewBatchService(uploadSvc, downloadSvc, metaFacade, localFS, compress)

	// === 后台任务 ===

	reaper := lifecycle.NewSessionReaper(metaFacade, localFS, cfg.Storage.TempDir, time.Minute)
	reaper.Start()
	defer reaper.Stop()

	scanner := lifecycle.NewConsistencyScanner(metaFacade, localFS, cfg.Storage.DataDir, cfg.Storage.TempDir)

	// 健康检查器
	healthChecker := &serverHealth{
		redis:   rdb,
		dataDir: cfg.Storage.DataDir,
		tempDir: cfg.Storage.TempDir,
		dbPath:  cfg.Database.Path,
	}

	// === 传输层 ===

	mw := transport.NewMiddleware().WithAuth(transport.AuthConfig{
		Enabled: cfg.Auth.Enabled,
		Token:   cfg.Auth.Token,
		Header:  cfg.Auth.Header,
	})

	// JWT 鉴权（仅在配置了密钥时启用）
	var authHandler *transport.AuthHandler
	if cfg.Auth.JWTSecret != "" {
		jwtExpiry := time.Duration(cfg.Auth.JWTExpiry) * time.Hour
		jwtSvc := auth.NewJWTService(cfg.Auth.JWTSecret, jwtExpiry, nil)
		mw.WithJWT(jwtSvc)
		authHandler = transport.NewAuthHandler(jwtSvc)
	}

	tusHandler := transport.NewTusHandler(uploadSvc)
	restHandler := transport.NewRESTHandler(uploadSvc, downloadSvc)
	downloadHandler := transport.NewDownloadHandler(downloadSvc)
	batchHandler := transport.NewBatchHandler(batchSvc)
	adminHandler := transport.NewAdminHandler(metaFacade, workerPool, cfg.Storage.DataDir, cfg.Storage.TempDir, cfg.Database.Path, cfg.Database.Type)
router := transport.NewRouter(mw, tusHandler, restHandler, downloadHandler, batchHandler, authHandler, adminHandler, uploadSvc, scanner, healthChecker)

	// 首次启动时执行一次快速巡检
	go func() {
		time.Sleep(10 * time.Second)
		if _, err := scanner.Scan(context.Background()); err != nil {
			log.Printf("首次巡检失败: %v", err)
		} else {
			log.Printf("首次巡检完成")
		}
	}()

	// === HTTP 服务 ===

	addr := cfg.Server.Addr
	srv := &http.Server{
		Addr:         addr,
		Handler:      router.Handler(),
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("服务启动于 %s (data=%s, db=%s)", addr, cfg.Storage.DataDir, cfg.Database.Path)
		log.Printf("上传配置: chunk_size=%d, workers=%d, ttl=%s",
			cfg.Upload.DefaultChunkSize, cfg.Upload.WorkerPoolSize, cfg.Upload.SessionTTL())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务异常退出: %v", err)
		}
	}()

	<-quit
	log.Println("收到关闭信号，正在优雅关闭...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("服务关闭超时: %v", err)
	}

	fmt.Println("服务已关闭")
}
