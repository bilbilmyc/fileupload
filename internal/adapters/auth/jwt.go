// Package auth 实现 domain.AuthService 端口
// 使用 golang-jwt/jwt/v5 签发和验证 JWT
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// JWTService JWT 鉴权实现
type JWTService struct {
	secret []byte
	expiry time.Duration
	users  map[string]*domain.AuthUser // 内存用户表（生产环境应从 DB 加载）

	refreshStore domain.RefreshTokenStore

	// usedRefresh is the single-process fallback for tests and standalone use.
	// Production wiring provides refreshStore so rotation is shared by instances.
	refreshMu   sync.Mutex
	usedRefresh map[string]time.Time
}

// NewJWTService 创建 JWT 鉴权服务
// secret: JWT 签名密钥
// expiry: access token 过期时间
// users: 预置用户列表。空列表不会隐式创建用户，避免生产环境暴露默认凭据。
func NewJWTService(secret string, expiry time.Duration, users []domain.AuthUser) *JWTService {
	return NewJWTServiceWithRefreshStore(secret, expiry, users, nil)
}

// NewJWTServiceWithRefreshStore 创建带共享 refresh token 消费存储的 JWT 服务。
// store 为空时保留单进程内存回退，便于独立运行和单元测试。
func NewJWTServiceWithRefreshStore(secret string, expiry time.Duration, users []domain.AuthUser, store domain.RefreshTokenStore) *JWTService {
	s := &JWTService{
		secret:       []byte(secret),
		expiry:       expiry,
		users:        make(map[string]*domain.AuthUser),
		refreshStore: store,
		usedRefresh:  make(map[string]time.Time),
	}

	for i := range users {
		s.users[users[i].Username] = &users[i]
	}

	return s
}

// DevelopmentUsers 返回仅供本地开发和测试使用的演示管理员。
// 调用方必须显式选择使用它；生产配置校验会拒绝 dev_admin_enabled。
func DevelopmentUsers() []domain.AuthUser {
	return []domain.AuthUser{{
		ID:        "u-admin",
		Username:  "admin",
		Password:  "admin123",
		Namespace: "default",
		Roles:     []string{"admin", "user"},
	}}
}

// Login 验证用户名密码并返回 JWT 令牌对
func (s *JWTService) Login(_ context.Context, username, password string) (*domain.TokenPair, error) {
	user, ok := s.users[username]
	if !ok {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	if strings.HasPrefix(user.Password, "$2") {
		if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
			return nil, fmt.Errorf("用户名或密码错误")
		}
	} else if user.Password != password {
		// 兼容仅供开发/测试的显式明文用户；生产配置只能传入 bcrypt 哈希。
		return nil, fmt.Errorf("用户名或密码错误")
	}

	return s.generateTokenPair(user)
}

// ValidateToken 验证 access token 并返回 claims
func (s *JWTService) ValidateToken(tokenStr string) (*domain.AuthClaims, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("非预期的签名方法: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("token 无效: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("token claims 无效")
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "access" {
		return nil, fmt.Errorf("非 access token")
	}

	userID, _ := claims["user_id"].(string)
	namespace, _ := claims["namespace"].(string)
	tokenID, _ := claims["token_id"].(string)
	if userID == "" || namespace == "" || tokenID == "" {
		return nil, fmt.Errorf("access token claims 缺少必需字段")
	}

	rolesRaw, ok := claims["roles"].([]any)
	if !ok {
		return nil, fmt.Errorf("access token roles 无效")
	}
	var roles []string
	for _, r := range rolesRaw {
		if s, ok := r.(string); ok {
			roles = append(roles, s)
		}
	}

	return &domain.AuthClaims{
		UserID:    userID,
		Namespace: namespace,
		Roles:     roles,
		TokenID:   tokenID,
	}, nil
}

// RefreshToken 使用 refresh token 获取新的令牌对
func (s *JWTService) RefreshToken(ctx context.Context, refreshToken string) (*domain.TokenPair, error) {
	// refresh token 也用 JWT 但不同 key 签发
	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("非预期的签名方法: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token 无效: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("refresh token claims 无效")
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		return nil, fmt.Errorf("非 refresh token")
	}

	userID, _ := claims["user_id"].(string)
	tokenID, _ := claims["token_id"].(string)
	if userID == "" || tokenID == "" {
		return nil, fmt.Errorf("refresh token 缺少必需字段")
	}
	expiresAt, ok := refreshExpiry(claims["exp"])
	if !ok {
		return nil, fmt.Errorf("refresh token 缺少有效 exp")
	}
	// 查找用户
	for _, user := range s.users {
		if user.ID == userID {
			if err := s.consumeRefreshToken(ctx, tokenID, expiresAt); err != nil {
				return nil, err
			}
			pair, err := s.generateTokenPair(user)
			if err != nil {
				return nil, err
			}
			return pair, nil
		}
	}

	return nil, fmt.Errorf("用户不存在")
}

func (s *JWTService) consumeRefreshToken(ctx context.Context, tokenID string, expiresAt time.Time) error {
	if s.refreshStore != nil {
		claimed, err := s.refreshStore.ClaimRefreshToken(ctx, tokenID, expiresAt)
		if err != nil {
			return fmt.Errorf("claim refresh token: %w", err)
		}
		if !claimed {
			return fmt.Errorf("refresh token 已被使用")
		}
		return nil
	}

	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	now := time.Now()
	for id, expiry := range s.usedRefresh {
		if !expiry.After(now) {
			delete(s.usedRefresh, id)
		}
	}
	if _, used := s.usedRefresh[tokenID]; used {
		return fmt.Errorf("refresh token 已被使用")
	}
	s.usedRefresh[tokenID] = expiresAt
	return nil
}

func refreshExpiry(value any) (time.Time, bool) {
	seconds, ok := value.(float64)
	if !ok || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(seconds), 0), true
}

// generateTokenPair 生成 access + refresh token 对
func (s *JWTService) generateTokenPair(user *domain.AuthUser) (*domain.TokenPair, error) {
	now := time.Now()
	tokenID := generateTokenID()
	expiresAt := now.Add(s.expiry)

	// Access Token
	accessClaims := jwt.MapClaims{
		"user_id":   user.ID,
		"username":  user.Username,
		"namespace": user.Namespace,
		"roles":     user.Roles,
		"token_id":  tokenID,
		"type":      "access",
		"iat":       now.Unix(),
		"exp":       expiresAt.Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("签发 access token 失败: %w", err)
	}

	// Refresh Token（更长过期时间）
	refreshExpiry := s.expiry * 2
	refreshClaims := jwt.MapClaims{
		"user_id":  user.ID,
		"token_id": generateTokenID(),
		"type":     "refresh",
		"iat":      now.Unix(),
		"exp":      now.Add(refreshExpiry).Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("签发 refresh token 失败: %w", err)
	}

	return &domain.TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int(s.expiry.Seconds()),
	}, nil
}

// generateTokenID 生成随机 token ID
func generateTokenID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
