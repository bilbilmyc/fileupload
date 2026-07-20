package transport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// AuthHandler 认证 HTTP 处理器
type AuthHandler struct {
	authSvc      domain.AuthService
	loginLimiter *sharePasswordLimiter
}

// NewAuthHandler 创建认证处理器
func NewAuthHandler(authSvc domain.AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc, loginLimiter: newSharePasswordLimiter(5, 15*time.Minute, nil)}
}

// withLoginLimiter 仅用于注入确定性测试策略。
func (h *AuthHandler) withLoginLimiter(limiter *sharePasswordLimiter) *AuthHandler {
	h.loginLimiter = limiter
	return h
}

// Login POST /v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req domain.LoginRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if req.Username == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	username := strings.ToLower(strings.TrimSpace(req.Username))
	client := shareClientAddress(r)
	if retry, allowed := h.loginLimiter.Allow(username, client); !allowed {
		respondLoginRateLimited(w, retry)
		return
	}
	pair, err := h.authSvc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if retry, locked := h.loginLimiter.RecordFailure(username, client); locked {
			respondLoginRateLimited(w, retry)
			return
		}
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "认证失败",
			"code":  "auth_failed",
		})
		return
	}
	h.loginLimiter.Reset(username, client)

	// 从 token 中解析用户信息
	claims, _ := h.authSvc.ValidateToken(pair.AccessToken)
	userID := ""
	namespace := "default"
	if claims != nil {
		userID = claims.UserID
		namespace = claims.Namespace
	}

	respondJSON(w, http.StatusOK, domain.LoginResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
		UserID:       userID,
		Namespace:    namespace,
	})
}

// Refresh POST /v1/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req domain.RefreshRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if req.RefreshToken == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	pair, err := h.authSvc.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "token 已失效",
			"code":  "token_expired",
		})
		return
	}

	respondJSON(w, http.StatusOK, pair)
}

// Me GET /v1/auth/me — 获取当前用户信息
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	// 从 context 中获取 claims（由 JWT 中间件注入）
	claims := GetAuthClaims(r.Context())
	if claims == nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "未认证",
			"code":  "auth_required",
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"user_id":   claims.UserID,
		"namespace": claims.Namespace,
		"roles":     claims.Roles,
	})
}

func respondLoginRateLimited(w http.ResponseWriter, retry time.Duration) {
	seconds := int64((retry + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	respondJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               "登录失败次数过多，请稍后重试",
		"code":                "login_rate_limited",
		"retry_after_seconds": seconds,
	})
}
