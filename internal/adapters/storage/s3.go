// Package storage 实现 domain.Storage 端口的 S3 适配器
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// S3Config S3 存储后端配置
type S3Config struct {
	Bucket         string // S3 bucket 名称
	Region         string // AWS region（如 us-east-1）
	Endpoint       string // 可选：自定义 endpoint（兼容 MinIO/S3）
	Prefix         string // 可选：key 前缀（如 "fileupload/"）
	ForcePathStyle bool   // 可选：路径式寻址（MinIO 需要）
}

// S3Storage S3 存储后端实现
type S3Storage struct {
	client *s3.Client
	cfg    S3Config
}

// NewS3Storage 创建 S3 存储后端
func NewS3Storage(ctx context.Context, cfg S3Config) (*S3Storage, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("S3 bucket 名称不能为空")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("加载 AWS 配置: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &S3Storage{client: client, cfg: cfg}, nil
}

// key 将逻辑路径转为 S3 key（含前缀）
func (s *S3Storage) key(path string) string {
	if s.cfg.Prefix == "" {
		return path
	}
	return strings.TrimRight(s.cfg.Prefix, "/") + "/" + strings.TrimLeft(path, "/")
}

// Write 从 reader 流式写入 S3
func (s *S3Storage) Write(ctx context.Context, p string, r io.Reader) (int64, error) {
	uploader := manager.NewUploader(s.client)
	cw := &countingReader{r: r}
	result, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.key(p)),
		Body:   cw,
	})
	if err != nil {
		return cw.n, fmt.Errorf("S3 上传失败: %w", err)
	}
	_ = result // Location 可用于日志
	return cw.n, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Open 读取 S3 对象，支持 Range
func (s *S3Storage) Open(ctx context.Context, p string, offset, length int64) (io.ReadCloser, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.key(p)),
	}
	if offset > 0 || length > 0 {
		rng := fmt.Sprintf("bytes=%d-", offset)
		if length > 0 {
			rng = fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
		}
		input.Range = aws.String(rng)
	}

	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("S3 读取失败: %w", err)
	}
	return result.Body, nil
}

// Delete 删除 S3 对象
func (s *S3Storage) Delete(ctx context.Context, p string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.key(p)),
	})
	if err != nil {
		return fmt.Errorf("S3 删除失败: %w", err)
	}
	return nil
}

// Stat 获取 S3 对象信息
func (s *S3Storage) Stat(ctx context.Context, p string) (int64, bool, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.key(p)),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return 0, false, nil
		}
		// 404 也视为不存在
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("S3 stat 失败: %w", err)
	}
	return *result.ContentLength, true, nil
}

// s3FileInfo 实现 fs.FileInfo，用于 Walk 回调
type s3FileInfo struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
}

func (f s3FileInfo) Name() string       { return f.name }
func (f s3FileInfo) Size() int64        { return f.size }
func (f s3FileInfo) Mode() fs.FileMode  { return 0644 }
func (f s3FileInfo) ModTime() time.Time { return f.modTime }
func (f s3FileInfo) IsDir() bool        { return f.isDir }
func (f s3FileInfo) Sys() any           { return nil }

// Walk 遍历 S3 bucket，fn 收到相对于 prefix 的路径
func (s *S3Storage) Walk(ctx context.Context, fn func(path string, info fs.FileInfo) error) error {
	prefix := s.key("")
	// 确保 prefix 以 / 结尾，便于列举
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.cfg.Bucket),
		Prefix: aws.String(prefix),
	})

	seenDirs := make(map[string]bool)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("S3 列举失败: %w", err)
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			// 去掉 prefix 得到相对路径
			relPath := strings.TrimPrefix(key, prefix)

			// 跳过 prefix 自身
			if relPath == "" {
				continue
			}

			// 通知目录项（S3 不返回目录，通过 key 中的 / 推断）
			parts := strings.Split(relPath, "/")
			for i := 0; i < len(parts)-1; i++ {
				dirPath := strings.Join(parts[:i+1], "/")
				if !seenDirs[dirPath] {
					seenDirs[dirPath] = true
					if err := fn(dirPath, s3FileInfo{name: dirPath, isDir: true}); err != nil {
						return err
					}
				}
			}

			// 通知文件本身
			if err := fn(relPath, s3FileInfo{
				name:    relPath,
				size:    *obj.Size,
				modTime: *obj.LastModified,
			}); err != nil {
				return err
			}
		}
	}

	// 排序确保目录在文件之前（向上通知）
	return nil
}

// EnsureBucket 确保 bucket 存在（首次部署时调用）
func (s *S3Storage) EnsureBucket(ctx context.Context) error {
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.cfg.Bucket),
	})
	if err != nil {
		// 如果已存在则忽略
		var alreadyOwned *types.BucketAlreadyOwnedByYou
		if errors.As(err, &alreadyOwned) {
			return nil
		}
		return fmt.Errorf("创建 S3 bucket 失败: %w", err)
	}
	return nil
}

// compile check
var _ domain.Storage = (*S3Storage)(nil)

// HealthCheck 检查 S3 bucket 是否可达。
func (s *S3Storage) HealthCheck(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.cfg.Bucket)})
	if err != nil {
		return fmt.Errorf("S3 bucket 不可达: %w", err)
	}
	return nil
}
