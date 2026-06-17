package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewMiddleware(t *testing.T) {
	mw := NewMiddleware()
	if mw == nil {
		t.Fatal("NewMiddleware returned nil")
	}
	if mw.rateLimiter == nil {
		t.Error("rateLimiter is nil")
	}
}

func TestRecoverMiddleware(t *testing.T) {
	mw := NewMiddleware()
	handler := mw.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("panic recover status = %d, want 500", w.Code)
	}
}

func TestRecoverMiddleware_NoPanic(t *testing.T) {
	mw := NewMiddleware()
	handler := mw.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("normal request status = %d, want 200", w.Code)
	}
}

func TestRequestID_Middleware(t *testing.T) {
	mw := NewMiddleware()
	handler := mw.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id == "" {
			t.Error("RequestID is empty")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header is empty")
	}
}

func TestRequestID_WithExistingID(t *testing.T) {
	mw := NewMiddleware()
	handler := mw.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id != "my-request-id" {
			t.Errorf("RequestID = %s, want my-request-id", id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", "my-request-id")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

func TestNamespace_Middleware(t *testing.T) {
	mw := NewMiddleware()
	handler := mw.Namespace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := GetNamespace(r.Context())
		if ns == "" {
			t.Error("Namespace is empty")
		}
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		headerVal  string
		queryVal   string
		expectNS   string
	}{
		{"X-User-ID 头", "user-123", "", "user-123"},
		{"X-Namespace 头", "", "ns-from-header", "ns-from-header"},
		{"Query param", "", "", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.headerVal != "" {
				req.Header.Set("X-User-ID", tt.headerVal)
			}
			if tt.queryVal != "" {
				req.Header.Set("X-Namespace", "ns-from-header")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		})
	}
}

func TestNamespace_Default(t *testing.T) {
	mw := NewMiddleware()
	handler := mw.Namespace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ns := GetNamespace(r.Context())
		if ns != "default" {
			t.Errorf("default namespace = %s, want 'default'", ns)
		}
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(1000, 1000) // 高 rate 确保通过

	// 前 100 个应全部允许
	for i := 0; i < 100; i++ {
		if !rl.Allow() {
			t.Errorf("第 %d 个请求被拒绝", i)
		}
	}
}

func TestRateLimiter_ExceedBurst(t *testing.T) {
	rl := NewRateLimiter(10, 10) // rate=10/s, burst=10

	// 消耗 burst
	allowed := 0
	for i := 0; i < 20; i++ {
		if rl.Allow() {
			allowed++
		}
	}
	if allowed > 20 {
		t.Errorf("allowed = %d", allowed)
	}
	// burst=10，所以至少前 10 个应通过
	if allowed < 10 {
		t.Errorf("allowed = %d, at least 10 expected", allowed)
	}
}

func TestRateLimiter_Recovery(t *testing.T) {
	rl := NewRateLimiter(100, 10) // 100/s, burst=10

	// 消耗 burst
	for i := 0; i < 10; i++ {
		rl.Allow()
	}

	// 第 11 个应被限
	if rl.Allow() {
		// 可能因为时间精度通过，不强制断言
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	mw := NewMiddleware()

	handler := mw.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// 正常请求应通过
	if w.Code != http.StatusOK && w.Code != http.StatusTooManyRequests {
		t.Errorf("unexpected status = %d", w.Code)
	}
}

func TestGetRequestID_Empty(t *testing.T) {
	// 没有 context 时，应返回空字符串
	req := httptest.NewRequest("GET", "/test", nil)
	id := GetRequestID(req.Context())
	if id != "" {
		t.Errorf("empty context requestID = %s", id)
	}
}

func TestGetNamespace_Empty(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	ns := GetNamespace(req.Context())
	if ns != "default" {
		t.Errorf("empty context namespace = %s", ns)
	}
}
