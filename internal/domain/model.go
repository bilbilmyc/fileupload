// Package domain 包含领域模型类型与核心业务接口。
// 这是系统的"内核"，不依赖任何外部包。
package domain

import "time"

// --- 上传会话 ---

// SessionStatus 上传会话状态
type SessionStatus string

const (
	SessionActive     SessionStatus = "active"
	SessionFinalizing SessionStatus = "finalizing"
	SessionCompleted  SessionStatus = "completed"
	SessionAborted    SessionStatus = "aborted"
)

// CompressionFormat 压缩格式
type CompressionFormat string

const (
	CompNone   CompressionFormat = "none"
	CompZstd   CompressionFormat = "zstd"
	CompGzip   CompressionFormat = "gzip"
	CompTarZst CompressionFormat = "tar.zst"
	CompTarGz  CompressionFormat = "tar.gz"
	CompZip    CompressionFormat = "zip"
)

// UploadSession 一个上传会话，对应一个文件的完整上传过程
type UploadSession struct {
	SessionID    string          `json:"session_id"`
	FileID       string          `json:"file_id,omitempty"` // Finalize 后赋值
	SHA256       string          `json:"sha256"`            // 客户端声明的原始内容 SHA-256
	UploadLength int64           `json:"upload_length"`     // 总字节数
	Compression  CompressionFormat `json:"compression"`
	ChunkSize    int64           `json:"chunk_size"`
	Namespace    string          `json:"namespace"`
	FileName     string          `json:"file_name,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	ExpireAt     time.Time        `json:"expire_at"`
	Status       SessionStatus   `json:"status"`
}

// ChunkInfo 单个分片信息
type ChunkInfo struct {
	Index   int    `json:"index"`
	SHA256  string `json:"sha256"` // 分片 SHA-256（压缩后）
	Size    int64  `json:"size"`
}

// --- 内容寻址去重 ---

// ContentBlob 物理文件（内容寻址）
type ContentBlob struct {
	SHA256      string    `json:"sha256"`       // 原始内容 SHA-256（主键）
	StoragePath string    `json:"storage_path"` // <namespace>/<fileID>（相对于 Storage root）
	Size        int64     `json:"size"`
	RefCount    int       `json:"ref_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// --- 逻辑文件/目录 ---

// FileMetadata 逻辑文件或目录节点
type FileMetadata struct {
	FileID    string    `json:"file_id"`
	SHA256    string    `json:"sha256,omitempty"` // 文件指向 content_blob；目录为空
	Name      string    `json:"name"`
	Path      string    `json:"path"` // 含目录相对路径
	Size      int64     `json:"size"`
	Namespace string    `json:"namespace"`
	IsDir     bool      `json:"is_dir"`
	ParentID  string    `json:"parent_id,omitempty"` // 自引用目录树
	Tags      []string  `json:"tags,omitempty"`      // 文件标签
	CreatedAt time.Time `json:"created_at"`
}

// --- 目录 Manifest ---

// DirManifest 目录上传时客户端提交的清单
type DirManifest struct {
	Name    string     `json:"name,omitempty"` // 目录名，为空时自动生成
	Entries []DirEntry `json:"entries"`
}

// DirEntry 目录中的一个文件项
type DirEntry struct {
	Path   string `json:"path"`   // 相对于目录的路径
	FileID string `json:"file_id"`
}

// --- 下载请求 ---

// DownloadRange 可选的文件范围
type DownloadRange struct {
	Offset int64 // 起始偏移
	Length int64 // 0 表示到文件尾
}

// IsZero 是否未指定范围
func (r DownloadRange) IsZero() bool {
	return r.Offset == 0 && r.Length == 0
}

// --- 领域错误（typed errors） ---

// DomainError 领域错误类型
type DomainError string

func (e DomainError) Error() string { return string(e) }

const (
	ErrSliceChecksum    DomainError = "分片校验和不匹配"
	ErrContentChecksum  DomainError = "整体校验和不匹配"
	ErrSessionNotFound  DomainError = "会话不存在或已过期"
	ErrSessionState     DomainError = "会话状态不允许此操作"
	ErrOffsetConflict   DomainError = "偏移量冲突（分片重叠）"
	ErrForbidden        DomainError = "namespace 无权访问"
	ErrBusy             DomainError = "服务忙，请稍后重试"
	ErrStorage          DomainError = "存储操作失败"
	ErrCorrupted        DomainError = "文件已损坏"
	ErrNotFound         DomainError = "资源不存在"
	ErrInvalidArgument  DomainError = "参数不合法"
	ErrPathTraversal    DomainError = "路径穿越被拒绝"
)
