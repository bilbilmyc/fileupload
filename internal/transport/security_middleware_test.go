package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/adapters/auth"
	"github.com/bilbilmyc/fileupload/internal/domain"
)

func TestNamespace_UsesJWTClaimInsteadOfRequestHeader(t *testing.T) {
	users := []domain.AuthUser{{ID: "u-1", Username: "reader", Password: "password", Namespace: "tenant-a", Roles: []string{"user"}}}
	jwtSvc := auth.NewJWTService("test-secret", time.Hour, users)
	pair, err := jwtSvc.Login(context.Background(), "reader", "password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	mw := NewMiddleware().WithJWT(jwtSvc).WithAuth(AuthConfig{Enforce: true})
	h := mw.JWTValidate(mw.Namespace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := GetNamespace(r.Context()); got != "tenant-a" {
			t.Errorf("namespace = %q, want tenant-a", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/v1/ls", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	req.Header.Set("X-Namespace", "tenant-b")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestNamespace_AdminMaySelectNamespace(t *testing.T) {
	mw := NewMiddleware()
	h := mw.Namespace(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := GetNamespace(r.Context()); got != "tenant-b" {
			t.Errorf("namespace = %q, want tenant-b", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	ctx := context.WithValue(context.Background(), ctxKeyAuthClaims, &domain.AuthClaims{Namespace: "tenant-a", Roles: []string{"admin"}})
	req := httptest.NewRequest(http.MethodGet, "/v1/ls", nil).WithContext(ctx)
	req.Header.Set("X-Namespace", "tenant-b")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestJWTValidate_EnforceRejectsMissingToken(t *testing.T) {
	mw := NewMiddleware().WithJWT(auth.NewJWTService("test-secret", time.Hour, nil)).WithAuth(AuthConfig{Enforce: true})
	h := mw.JWTValidate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/ls", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRequireRole(t *testing.T) {
	mw := NewMiddleware()
	h := mw.RequireRole("admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	for _, tc := range []struct {
		name string
		ctx  context.Context
		want int
	}{
		{name: "anonymous", ctx: context.Background(), want: http.StatusUnauthorized},
		{name: "user", ctx: context.WithValue(context.Background(), ctxKeyAuthClaims, &domain.AuthClaims{Roles: []string{"user"}}), want: http.StatusForbidden},
		{name: "admin", ctx: context.WithValue(context.Background(), ctxKeyAuthClaims, &domain.AuthClaims{Roles: []string{"admin"}}), want: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/admin/status", nil).WithContext(tc.ctx))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestMetricsAuth(t *testing.T) {
	mw := NewMiddleware().WithObservability(false, "metrics-token-which-is-long-enough")
	h := mw.MetricsAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	unauthorized := httptest.NewRecorder()
	h.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	authorizedReq.Header.Set("Authorization", "Bearer metrics-token-which-is-long-enough")
	authorized := httptest.NewRecorder()
	h.ServeHTTP(authorized, authorizedReq)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d, want %d", authorized.Code, http.StatusNoContent)
	}
}

func TestSecurityHeaders(t *testing.T) {
	mw := NewMiddleware()
	h := mw.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := w.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("Content-Security-Policy is missing")
	}
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS must not be sent over plain HTTP")
	}
}

func TestRequestBodyLimit(t *testing.T) {
	mw := NewMiddleware()
	h := mw.RequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			respondError(w, http.StatusRequestEntityTooLarge, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	large := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(make([]byte, (1<<20)+1)))
	largeW := httptest.NewRecorder()
	h.ServeHTTP(largeW, large)
	if largeW.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large control request status = %d, want 413", largeW.Code)
	}

	upload := httptest.NewRequest(http.MethodPatch, "/uploads/session-1", bytes.NewReader(make([]byte, (1<<20)+1)))
	uploadW := httptest.NewRecorder()
	h.ServeHTTP(uploadW, upload)
	if uploadW.Code != http.StatusNoContent {
		t.Fatalf("upload data request status = %d, want 204", uploadW.Code)
	}
}
