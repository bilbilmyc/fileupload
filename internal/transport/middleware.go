// Package transport 实现 HTTP 传输层
// 包含路由器、中间件、tus/REST/下载 handler。
package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mayc/casdao/fileupload/internal/domain"
)

// ctxKey 用于 context 的键类型
type ctxKey string

const (
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyNamespace ctxKey = "namespace"
)

// Middleware 中间件集合
type Middleware struct {
	rateLimiter *RateLimiter
}

// NewMiddleware 创建中间件集合
func NewMiddleware() *Middleware {
	return &Middleware{
		rateLimiter: NewRateLimiter(100, 200),
	}
}

// Recover panic 恢复中间件
func (m *Middleware) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[panic] %v (request %s %s)", rec, r.Method, r.URL.Path)
				http.Error(w, `{"error":"内部错误"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequestID 注入请求 ID
func (m *Middleware) RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Namespace 从请求头读取 namespace（由上游网关注入）
func (m *Middleware) Namespace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := r.Header.Get("X-User-ID")
		if ns == "" {
			ns = r.Header.Get("X-Namespace")
		}
		if ns == "" {
			ns = r.URL.Query().Get("namespace")
		}
		if ns == "" {
			ns = "default"
		}
		ctx := context.WithValue(r.Context(), ctxKeyNamespace, ns)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RateLimit 令牌桶限流
func (m *Middleware) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.rateLimiter.Allow() {
			w.Header().Set("Retry-After", "1")
			respondJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "请求过于频繁",
				"code":  "rate_limited",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter 简单令牌桶
type RateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	rate     float64
	burst    float64
	lastTime time.Time
}

// NewRateLimiter 创建令牌桶限流器
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{
		tokens:   burst,
		rate:     rate,
		burst:    burst,
		lastTime: time.Now(),
	}
}

// Allow 是否允许一个请求通过
func (l *RateLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastTime).Seconds()
	l.lastTime = now
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}

	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// ---- 辅助函数 ----

// GetNamespace 从 context 中获取 namespace
func GetNamespace(ctx context.Context) string {
	if ns, ok := ctx.Value(ctxKeyNamespace).(string); ok {
		return ns
	}
	return "default"
}

// GetRequestID 从 context 中获取 requestID
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return id
	}
	return ""
}

// respondJSON 写 JSON 响应
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError 写 JSON 错误
func respondError(w http.ResponseWriter, status int, err error) {
	errMsg := err.Error()
	if domainErr, ok := err.(domain.DomainError); ok {
		errMsg = string(domainErr)
	}
	respondJSON(w, status, map[string]string{"error": errMsg})
}

// domainErrorToStatus 领域错误转 HTTP 状态码
func domainErrorToStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch err {
	case domain.ErrSliceChecksum:
		return 460
	case domain.ErrContentChecksum:
		return http.StatusUnprocessableEntity
	case domain.ErrSessionNotFound:
		return http.StatusNotFound
	case domain.ErrSessionState:
		return http.StatusConflict
	case domain.ErrOffsetConflict:
		return http.StatusConflict
	case domain.ErrForbidden:
		return http.StatusForbidden
	case domain.ErrBusy:
		return http.StatusServiceUnavailable
	case domain.ErrCorrupted:
		return http.StatusGone
	case domain.ErrNotFound:
		return http.StatusNotFound
	case domain.ErrInvalidArgument:
		return http.StatusBadRequest
	case domain.ErrPathTraversal:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
