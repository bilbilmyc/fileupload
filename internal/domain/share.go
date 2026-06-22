package domain

// ShareEntry 分享链接
type ShareEntry struct {
	Token         string `json:"token"`
	FileID        string `json:"file_id"`
	PasswordHash  string `json:"-"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	MaxDownloads  int    `json:"max_downloads"`
	CurDownloads  int    `json:"cur_downloads"`
	Namespace     string `json:"namespace"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// CreateShareRequest 创建分享请求
type CreateShareRequest struct {
	FileID       string `json:"file_id"`
	Password     string `json:"password,omitempty"`
	ExpiresIn    int    `json:"expires_in"`    // 过期小时数，0=不限
	MaxDownloads int    `json:"max_downloads"` // 0=不限
}
