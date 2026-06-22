// Package auth 实现 domain.AuthService 端口
// 使用 golang-jwt/jwt/v5 签发和验证 JWT
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/bilbilmyc/fileupload/internal/domain"
)

// JWTService JWT 鉴权实现
type JWTService struct {
	secret     []byte
	expiry     time.Duration
	users      map[string]*domain.AuthUser // 内存用户表（生产环境应从 DB 加载）
}

// NewJWTService 创建 JWT 鉴权服务
// secret: JWT 签名密钥
// expiry: access token 过期时间
// users: 预置用户列表（nil 则使用默认 admin 用户）
func NewJWTService(secret string, expiry time.Duration, users []domain.AuthUser) *JWTService {
	s := &JWTService{
		secret: []byte(secret),
		expiry: expiry,
		users:  make(map[string]*domain.AuthUser),
	}

	// 添加预置用户
	if len(users) > 0 {
		for i := range users {
			s.users[users[i].Username] = &users[i]
		}
	} else {
		// 默认 admin 用户（开发/演示用）
		s.users["admin"] = &domain.AuthUser{
			ID:        "u-admin",
			Username:  "admin",
			Password:  "admin123", // 明文，生产环境用 bcrypt
			Namespace: "default",
			Roles:     []string{"admin", "user"},
		}
	}

	return s
}

// Login 验证用户名密码并返回 JWT 令牌对
func (s *JWTService) Login(_ context.Context, username, password string) (*domain.TokenPair, error) {
	user, ok := s.users[username]
	if !ok {
		return nil, fmt.Errorf("用户名或密码错误")
	}
	if user.Password != password {
		return nil, fmt.Errorf("用户名或密码错误")
	}

	return s.generateTokenPair(user)
}

// ValidateToken 验证 access token 并返回 claims
func (s *JWTService) ValidateToken(tokenStr string) (*domain.AuthClaims, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
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

	userID, _ := claims["user_id"].(string)
	namespace, _ := claims["namespace"].(string)
	tokenID, _ := claims["token_id"].(string)

	rolesRaw, _ := claims["roles"].([]any)
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
func (s *JWTService) RefreshToken(_ context.Context, refreshToken string) (*domain.TokenPair, error) {
	// refresh token 也用 JWT 但不同 key 签发
	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期的签名方法")
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
	// 查找用户
	for _, user := range s.users {
		if user.ID == userID {
			return s.generateTokenPair(user)
		}
	}

	return nil, fmt.Errorf("用户不存在")
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
