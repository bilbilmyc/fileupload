package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/adapters/auth"
)

func TestJWTValidate_SkippedWhenNoAuthSvc(t *testing.T) {
	mw := NewMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	mw.JWTValidate(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestJWTValidate_NoHeaderPassesThrough(t *testing.T) {
	mw := NewMiddleware()
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, nil)
	mw.WithJWT(jwtSvc)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetAuthClaims(r.Context())
		if claims != nil {
			t.Error("expected no claims without token")
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	mw.JWTValidate(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestJWTValidate_ValidToken(t *testing.T) {
	mw := NewMiddleware()
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, nil)
	mw.WithJWT(jwtSvc)

	pair, _ := jwtSvc.Login(context.Background(), "admin", "admin123")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := GetAuthClaims(r.Context())
		if claims == nil {
			t.Fatal("expected claims to be injected")
		}
		if claims.UserID != "u-admin" {
			t.Errorf("UserID = %s, want u-admin", claims.UserID)
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	mw.JWTValidate(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestJWTValidate_InvalidToken(t *testing.T) {
	mw := NewMiddleware()
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, nil)
	mw.WithJWT(jwtSvc)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	mw.JWTValidate(handler).ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestJWTValidate_NonBearerHeader(t *testing.T) {
	mw := NewMiddleware()
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, nil)
	mw.WithJWT(jwtSvc)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 使用 X-Auth-Token 而不是 Bearer，应通过
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "NotBearer token")
	w := httptest.NewRecorder()
	mw.JWTValidate(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
