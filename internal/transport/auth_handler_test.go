package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/adapters/auth"
	"github.com/bilbilmyc/fileupload/internal/domain"
)

func TestAuthHandler_Login_Success(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	body, _ := json.Marshal(domain.LoginRequest{Username: "admin", Password: "admin123"})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp domain.LoginResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if resp.UserID != "u-admin" {
		t.Errorf("UserID = %s, want u-admin", resp.UserID)
	}
}

func TestAuthHandler_Login_WrongPassword(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	body, _ := json.Marshal(domain.LoginRequest{Username: "admin", Password: "wrong"})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthHandler_Login_EmptyFields(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	tests := []domain.LoginRequest{
		{Username: "", Password: "admin123"},
		{Username: "admin", Password: ""},
		{Username: "", Password: ""},
	}
	for _, tc := range tests {
		body, _ := json.Marshal(tc)
		req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler.Login(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("Login(%+v) status = %d, want 400", tc, w.Code)
		}
	}
}

func TestAuthHandler_Refresh(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	// 先登录获取 refresh token
	loginBody, _ := json.Marshal(domain.LoginRequest{Username: "admin", Password: "admin123"})
	loginReq := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	loginW := httptest.NewRecorder()
	handler.Login(loginW, loginReq)

	var loginResp domain.LoginResponse
	json.NewDecoder(loginW.Body).Decode(&loginResp)

	// 使用 refresh token
	refreshBody, _ := json.Marshal(domain.RefreshRequest{RefreshToken: loginResp.RefreshToken})
	req := httptest.NewRequest("POST", "/v1/auth/refresh", bytes.NewReader(refreshBody))
	w := httptest.NewRecorder()
	handler.Refresh(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthHandler_Refresh_Invalid(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	body, _ := json.Marshal(domain.RefreshRequest{RefreshToken: "invalid-token"})
	req := httptest.NewRequest("POST", "/v1/auth/refresh", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.Refresh(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthHandler_Me_WithClaims(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	// 先登录获取 token
	loginBody, _ := json.Marshal(domain.LoginRequest{Username: "admin", Password: "admin123"})
	loginReq := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	loginW := httptest.NewRecorder()
	handler.Login(loginW, loginReq)

	var loginResp domain.LoginResponse
	json.NewDecoder(loginW.Body).Decode(&loginResp)

	// 使用 JWT middleware 验证并注入 claims
	claims, _ := jwtSvc.ValidateToken(loginResp.AccessToken)
	req := httptest.NewRequest("GET", "/v1/auth/me", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, ctxKeyAuthClaims, claims)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.Me(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuthHandler_Me_NoClaims(t *testing.T) {
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc)

	req := httptest.NewRequest("GET", "/v1/auth/me", nil)
	w := httptest.NewRecorder()
	handler.Me(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthHandler_LoginRateLimitAndReset(t *testing.T) {
	now := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	limiter := newSharePasswordLimiter(2, time.Minute, func() time.Time { return now })
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, auth.DevelopmentUsers())
	handler := NewAuthHandler(jwtSvc).withLoginLimiter(limiter)

	login := func(password string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(domain.LoginRequest{Username: "admin", Password: password})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "198.51.100.5:43123"
		w := httptest.NewRecorder()
		handler.Login(w, req)
		return w
	}

	if got := login("wrong").Code; got != http.StatusUnauthorized {
		t.Fatalf("first failure status = %d, want 401", got)
	}
	limited := login("wrong")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited response = %d retry=%q", limited.Code, limited.Header().Get("Retry-After"))
	}
	if got := login("admin123").Code; got != http.StatusTooManyRequests {
		t.Fatalf("locked login status = %d, want 429", got)
	}

	now = now.Add(time.Minute)
	if got := login("admin123").Code; got != http.StatusOK {
		t.Fatalf("login after cooldown status = %d, want 200", got)
	}
	if got := login("wrong").Code; got != http.StatusUnauthorized {
		t.Fatalf("failure after reset status = %d, want 401", got)
	}
}
