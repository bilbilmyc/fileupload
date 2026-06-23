// Package transport 实现 HTTP 传输层
// 包含路由器、中间件、tus/REST/下载 handler。
package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ctxKey 用于 context 的键类型
type ctxKey string

const (
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyNamespace ctxKey = "namespace"
	ctxKeyAuthClaims ctxKey = "auth_claims"
)

// Middleware 中间件集合
type Middleware struct {
	rateLimiter *RateLimiterGroup
	authCfg     AuthConfig
	authSvc     domain.AuthService // JWT 鉴权服务
}

// AuthConfig 认证中间件配置
type AuthConfig struct {
	Enabled bool
	Token   string
	Header  string
}

// NewMiddleware 创建中间件集合
func NewMiddleware() *Middleware {
	return &Middleware{
		rateLimiter: NewRateLimiterGroup(100, 200),
	}
}

// WithAuth 设置静态 Token 认证配置（链式调用）
func (m *Middleware) WithAuth(cfg AuthConfig) *Middleware {
	m.authCfg = cfg
	return m
}

// WithJWT 设置 JWT 鉴权服务（链式调用）
func (m *Middleware) WithJWT(authSvc domain.AuthService) *Middleware {
	m.authSvc = authSvc
	return m
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

// RateLimit 令牌桶限流（按 namespace / IP 隔离）
// 上传和健康检查路径豁免限流，避免大量小文件并发上传被误拦。
func (m *Middleware) RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 上传路径和健康检查豁免限流
		path := r.URL.Path
		if strings.HasPrefix(path, "/uploads") || strings.HasPrefix(path, "/v1/uploads") || path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		key := m.rateKey(r)
		if !m.rateLimiter.Allow(key) {
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

// rateKey 生成限流 key：namespace 优先，其次 RemoteAddr
func (m *Middleware) rateKey(r *http.Request) string {
	ns := GetNamespace(r.Context())
	if ns != "" && ns != "default" {
		return "ns:" + ns
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	return "ip:" + ip
}

// RateLimiterGroup 按 key 隔离的令牌桶限流器组
type RateLimiterGroup struct {
	mu       sync.RWMutex
	limiters map[string]*RateLimiter
	rate     float64
	burst    float64
}

// NewRateLimiterGroup 创建限流器组
func NewRateLimiterGroup(rate, burst float64) *RateLimiterGroup {
	return &RateLimiterGroup{
		limiters: make(map[string]*RateLimiter),
		rate:     rate,
		burst:    burst,
	}
}

// Allow 获取 key 对应的限流器并尝试放行
func (g *RateLimiterGroup) Allow(key string) bool {
	g.mu.RLock()
	l, ok := g.limiters[key]
	g.mu.RUnlock()
	if ok {
		return l.Allow()
	}

	g.mu.Lock()
	l, ok = g.limiters[key]
	if !ok {
		l = NewRateLimiter(g.rate, g.burst)
		g.limiters[key] = l
	}
	g.mu.Unlock()
	return l.Allow()
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

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Auth X-Auth-Token 认证中间件
func (m *Middleware) Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.authCfg.Enabled || m.authCfg.Token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 健康检查和前端静态资源免认证
		path := r.URL.Path
		if path == "/health" || path == "/" {
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get(m.authCfg.Header)
		if token == "" {
			respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "缺少认证令牌",
				"code":  "auth_required",
			})
			return
		}
		if token != m.authCfg.Token {
			respondJSON(w, http.StatusForbidden, map[string]string{
				"error": "认证令牌无效",
				"code":  "auth_invalid",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Logging 请求日志中间件，记录方法、路径、状态码、耗时
func (m *Middleware) Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		elapsed := time.Since(start)
		log.Printf("[%s] %s %s → %d (%s)", r.Method, r.URL.Path, GetNamespace(r.Context()), rw.status, elapsed)
	})
}

// JWTValidate JWT 认证中间件 —— 验证 Authorization: Bearer <token> 并将 claims 注入 context
func (m *Middleware) JWTValidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.authSvc == nil {
			// JWT 未配置，跳过验证（兼容旧版）
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// 允许未认证请求通过（namespace 从 X-Namespace 头获取）
			next.ServeHTTP(w, r)
			return
		}

		// 解析 Bearer token
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenStr == authHeader {
			next.ServeHTTP(w, r)
			return
		}

		claims, err := m.authSvc.ValidateToken(tokenStr)
		if err != nil {
			respondJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "token 无效",
				"code":  "token_invalid",
			})
			return
		}

		// 从 token 中提取 namespace 注入 context，覆盖 X-Namespace 头
		ctx := context.WithValue(r.Context(), ctxKeyAuthClaims, claims)
		ns := claims.Namespace
		if ns != "" {
			ctx = context.WithValue(ctx, ctxKeyNamespace, ns)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- JWT Claims Context Helpers ----

// GetAuthClaims 从 context 获取 JWT claims
func GetAuthClaims(ctx context.Context) *domain.AuthClaims {
	if claims, ok := ctx.Value(ctxKeyAuthClaims).(*domain.AuthClaims); ok {
		return claims
	}
	return nil
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
	log.Printf("[error] %s (status=%d)", err.Error(), status)
	errMsg := err.Error()
	var domainErr domain.DomainError
	if errors.As(err, &domainErr) {
		errMsg = string(domainErr)
	}
	respondJSON(w, status, map[string]string{"error": errMsg})
}

// domainErrorToStatus 领域错误转 HTTP 状态码
func domainErrorToStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var de domain.DomainError
	if errors.As(err, &de) {
		switch de {
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
		case domain.ErrShareExhausted:
			return http.StatusGone
		case domain.ErrNotFound:
			return http.StatusNotFound
		case domain.ErrInvalidArgument:
			return http.StatusBadRequest
		case domain.ErrPathTraversal:
			return http.StatusBadRequest
		}
	}
	return http.StatusInternalServerError
}
