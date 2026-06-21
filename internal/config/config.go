// Package config 配置加载，支持 YAML（首选）+ 环境变量覆盖
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全部配置
type Config struct {
	Server   ServerConfig   `json:"server" yaml:"server"`
	Storage  StorageConfig  `json:"storage" yaml:"storage"`
	Redis    RedisConfig    `json:"redis" yaml:"redis"`
	Database DatabaseConfig `json:"database" yaml:"database"`
	Upload   UploadConfig   `json:"upload" yaml:"upload"`
	Download DownloadConfig `json:"download" yaml:"download"`
}

// ServerConfig HTTP 服务配置
type ServerConfig struct {
	Addr         string `json:"addr" yaml:"addr"`
	ReadTimeout  int    `json:"read_timeout" yaml:"read_timeout"`
	WriteTimeout int    `json:"write_timeout" yaml:"write_timeout"`
	IdleTimeout  int    `json:"idle_timeout" yaml:"idle_timeout"`
}

// StorageConfig 存储配置
type StorageConfig struct {
	Type    string `json:"type" yaml:"type"`        // local / s3
	DataDir string `json:"data_dir" yaml:"data_dir"` // local 模式数据目录
	TempDir string `json:"temp_dir" yaml:"temp_dir"`  // 临时分片目录
	S3      S3Config `json:"s3" yaml:"s3"`            // S3 模式配置
}

// S3Config S3 存储后端配置
type S3Config struct {
	Bucket         string `json:"bucket" yaml:"bucket"`
	Region         string `json:"region" yaml:"region"`
	Endpoint       string `json:"endpoint" yaml:"endpoint"`
	Prefix         string `json:"prefix" yaml:"prefix"`
	ForcePathStyle bool   `json:"force_path_style" yaml:"force_path_style"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string `json:"addr" yaml:"addr"`
	Password string `json:"password" yaml:"password"`
	DB       int    `json:"db" yaml:"db"`
	Prefix   string `json:"prefix" yaml:"prefix"`
}

// DatabaseConfig 冷数据库配置
type DatabaseConfig struct {
	Type string `json:"type" yaml:"type"`
	Path string `json:"path" yaml:"path"`
}

// UploadConfig 上传服务配置
type UploadConfig struct {
	SessionTTLMinutes int   `json:"session_ttl_minutes" yaml:"session_ttl_minutes"`
	DefaultChunkSize  int64 `json:"default_chunk_size" yaml:"default_chunk_size"`
	WorkerPoolSize    int   `json:"worker_pool_size" yaml:"worker_pool_size"`
	WorkerQueueSize   int   `json:"worker_queue_size" yaml:"worker_queue_size"`
}

// DownloadConfig 下载服务配置
type DownloadConfig struct {
	MaxArchiveSize int64 `json:"max_archive_size" yaml:"max_archive_size"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Addr:         ":8080",
			ReadTimeout:  30,
			WriteTimeout: 300,
			IdleTimeout:  60,
		},
		Storage: StorageConfig{
			Type:    "local",
			DataDir: "data",
			TempDir: "tmp",
		},
		Redis: RedisConfig{
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
			Prefix:   "upload:",
		},
		Database: DatabaseConfig{
			Type: "sqlite",
			Path: "fileupload.db",
		},
		Upload: UploadConfig{
			SessionTTLMinutes: 60,
			DefaultChunkSize:  10 * 1024 * 1024,
			WorkerPoolSize:    4,
			WorkerQueueSize:   100,
		},
		Download: DownloadConfig{
			MaxArchiveSize: 0,
		},
	}
}

// Load 加载配置。
// path 可以是目录或具体文件：
//   - 传目录：在该目录下查找 fileupload.yaml / fileupload.yml
//   - 传具体文件：直接解析该 YAML 文件
// 未指定 path 或不存在的文件：使用默认配置。
// 之后以环境变量覆盖。
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	// 解析配置文件路径
	yamlPath := resolveConfigPath(path)
	if yamlPath != "" {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			return cfg, fmt.Errorf("读取配置文件 %s: %w", yamlPath, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("解析 YAML %s: %w", yamlPath, err)
		}
	}

	// 环境变量覆盖
	loadEnv(&cfg)

	return cfg, nil
}

// resolveConfigPath 解析配置文件路径
func resolveConfigPath(path string) string {
	if path == "" {
		// 尝试默认位置
		for _, name := range []string{"fileupload.yaml", "fileupload.yml"} {
			if _, err := os.Stat(name); err == nil {
				return name
			}
		}
		return ""
	}

	// 传入的是目录
	info, err := os.Stat(path)
	if err != nil {
		return "" // 文件不存在也返回空，loadEnv 兜底
	}
	if info.IsDir() {
		for _, name := range []string{"fileupload.yaml", "fileupload.yml"} {
			full := filepath.Join(path, name)
			if _, err := os.Stat(full); err == nil {
				return full
			}
		}
		return ""
	}

	// 传入的是具体文件
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return "" // 不是 YAML 跳过
	}
	return path
}

// loadEnv 从环境变量加载配置覆盖
func loadEnv(cfg *Config) {
	if v := os.Getenv("FILEUPLOAD_SERVER_ADDR"); v != "" {
		cfg.Server.Addr = v
	}
	if v := os.Getenv("FILEUPLOAD_STORAGE_DATA_DIR"); v != "" {
		cfg.Storage.DataDir = v
	}
	if v := os.Getenv("FILEUPLOAD_STORAGE_TEMP_DIR"); v != "" {
		cfg.Storage.TempDir = v
	}
	if v := os.Getenv("FILEUPLOAD_REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := os.Getenv("FILEUPLOAD_REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("FILEUPLOAD_DB_PATH"); v != "" {
		cfg.Database.Path = v
	}
	if v := os.Getenv("FILEUPLOAD_SESSION_TTL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Upload.SessionTTLMinutes = n
		}
	}
	if v := os.Getenv("FILEUPLOAD_CHUNK_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.Upload.DefaultChunkSize = n
		}
	}
	if v := os.Getenv("FILEUPLOAD_WORKER_POOL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Upload.WorkerPoolSize = n
		}
	}
}

// SessionTTL 返回会话超时持续时间
func (u UploadConfig) SessionTTL() time.Duration {
	return time.Duration(u.SessionTTLMinutes) * time.Minute
}

// DumpYAML 导出当前配置为 YAML 字符串（方便查看/调试）
func (c Config) DumpYAML() (string, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("序列化配置: %w", err)
	}
	return string(data), nil
}
