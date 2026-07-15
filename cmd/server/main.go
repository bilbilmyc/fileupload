// Command server 是文件上传下载服务的 HTTP 服务端入口。
//
// 启动方式：
//
//	FILEUPLOAD_REDIS_ADDR=localhost:6379 go run ./cmd/server
//	FILEUPLOAD_DB_PATH=/tmp/fileupload.db go run ./cmd/server
//
// 默认配置：
//
//   - 监听 :8080
//   - 本地文件系统存储（data/）
//   - SQLite 数据库（fileupload.db）
//   - Redis（localhost:6379）
//
// main() 只负责：加载配置 → 装配依赖 → Build → 启动后台任务 →
// ListenAndServe + 优雅关闭。所有依赖装配逻辑在 builder.go 的 Build() 中。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	"github.com/bilbilmyc/fileupload/internal/transport"
)

func main() {
	cfg, err := config.Load(os.Getenv("FILEUPLOAD_CONFIG"))
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	deps, err := buildDeps(&cfg)
	if err != nil {
		log.Fatalf("装配依赖: %v", err)
	}

	srv, err := Build(deps)
	if err != nil {
		log.Fatalf("Build 服务: %v", err)
	}

	srv.Reaper().Start()
	srv.TrashJanitor().Start()
	defer srv.TrashJanitor().Stop()
	defer srv.Reaper().Stop()

	// 首次启动后做一次快速巡检
	go func() {
		time.Sleep(10 * time.Second)
		if _, err := srv.Scanner().Scan(context.Background()); err != nil {
			log.Printf("首次巡检失败: %v", err)
		} else {
			log.Printf("首次巡检完成")
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("服务启动于 %s (data=%s)", deps.ServerCfg.Addr, deps.UploadCfg.DataDir)
		if err := srv.HTTP().ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务异常退出: %v", err)
		}
	}()

	<-quit
	log.Println("收到关闭信号，正在优雅关闭...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.HTTP().Shutdown(ctx); err != nil {
		log.Printf("服务关闭超时: %v", err)
	}
	log.Println("服务已关闭")
}

// buildDeps 从配置构造 Deps。所有 log.Fatalf 上提为 error 返回，
// 让 Build 与 main 都可测试。
func buildDeps(cfg *config.Config) (Deps, error) {
	// 存储
	localFS, err := storage.NewLocalFS(cfg.Storage.DataDir)
	if err != nil {
		return Deps{}, must(err, "初始化本地存储")
	}
	tempFS, err := storage.NewLocalFS(cfg.Storage.TempDir)
	if err != nil {
		return Deps{}, must(err, "初始化临时存储")
	}

	// 元数据
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("警告: Redis 连接失败 (%v)，热数据功能受限", err)
	}
	redisStore := metadata.NewRedisStore(rdb, cfg.Redis.Prefix)

	var coldStore metadata.ColdStore
	if cfg.Database.Type == "postgres" {
		pgStore, err := metadata.NewPostgresStore(cfg.Database.PG.BuildDSN())
		if err != nil {
			return Deps{}, must(err, "初始化 PostgreSQL")
		}
		coldStore = pgStore
		log.Printf("使用 PostgreSQL 数据库")
	} else {
		sqliteStore, err := metadata.NewSQLiteStore(cfg.Database.Path)
		if err != nil {
			return Deps{}, must(err, "初始化 SQLite")
		}
		coldStore = sqliteStore
	}
	metaFacade := metadata.NewFacade(redisStore, coldStore)

	// 压缩与哈希
	compress, err := compressor.NewCompressor()
	if err != nil {
		return Deps{}, must(err, "初始化压缩器")
	}
	hash := hasher.NewSHA256Hasher()

	// JWT（可选）
	var authSvc domain.AuthService
	if cfg.Auth.JWTSecret != "" {
		jwtExpiry := time.Duration(cfg.Auth.JWTExpiry) * time.Hour
		authSvc = auth.NewJWTService(cfg.Auth.JWTSecret, jwtExpiry, nil)
	}

	workerPool := domain.NewSimpleWorkerPool(cfg.Upload.WorkerPoolSize, cfg.Upload.WorkerQueueSize)

	return Deps{
		Storage:        localFS,
		TempFS:         tempFS,
		Metadata:       metaFacade,
		Compressor:     compress,
		Hasher:         hash,
		WorkerPool:     workerPool,
		Auth:           authSvc,
		UploadCfg:      domain.UploadConfig{SessionTTL: cfg.Upload.SessionTTL(), DataDir: cfg.Storage.DataDir, DefaultChunkSize: cfg.Upload.DefaultChunkSize, NamespaceQuotaBytes: cfg.Upload.NamespaceQuotaBytes},
		DownloadCfg:    domain.DownloadConfig{DataDir: cfg.Storage.DataDir},
		AuthCfg:        transport.AuthConfig{Enabled: cfg.Auth.Enabled, Token: cfg.Auth.Token, Header: cfg.Auth.Header},
		CORSOrigins:    cfg.CORS.AllowedOrigins,
		ServerCfg:      ServerConfig{Addr: cfg.Server.Addr, ReadTimeout: time.Duration(cfg.Server.ReadTimeout) * time.Second, WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second, IdleTimeout: time.Duration(cfg.Server.IdleTimeout) * time.Second},
		ReaperInterval: time.Minute,
		TrashRetention: time.Duration(cfg.Upload.TrashRetentionHours) * time.Hour,
	}, nil
}
