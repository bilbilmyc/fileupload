package domain

import (
	"context"
	"time"
)

// ============================================================
// 鉴权领域模型和端口接口
// ============================================================

// RefreshTokenStore 提供跨进程的一次性 refresh token 消费语义。
// tokenID 应在 expiresAt 到期后自动失效，ClaimRefreshToken 必须具备原子性。
type RefreshTokenStore interface {
	ClaimRefreshToken(ctx context.Context, tokenID string, expiresAt time.Time) (bool, error)
}

// AuthService 鉴权服务接口
type AuthService interface {
	// Login 验证 credentials 并返回 token 对
	Login(ctx context.Context, username, password string) (*TokenPair, error)
	// ValidateToken 验证 access token 并返回 claims
	ValidateToken(tokenStr string) (*AuthClaims, error)
	// RefreshToken 使用 refresh token 获取新的 token 对
	RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error)
}

// TokenPair 令牌对
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // 过期秒数
}

// AuthClaims JWT claims 结构
type AuthClaims struct {
	UserID    string   `json:"user_id"`
	Namespace string   `json:"namespace"`
	Roles     []string `json:"roles"`
	TokenID   string   `json:"token_id"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse 登录响应
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	UserID       string `json:"user_id"`
	Namespace    string `json:"namespace"`
}

// RefreshRequest 刷新 token 请求
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// AuthUser 认证用户（可扩展为从 DB 加载）
type AuthUser struct {
	ID        string
	Username  string
	Password  string // bcrypt 哈希
	Namespace string
	Roles     []string
}
