package fileupload

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonResp 辅助：在 httptest server 中写 JSON 响应
func jsonResp(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// TestLogin_Success RED → impl → GREEN
func TestLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/login" {
			t.Errorf("path = %s, want /v1/auth/login", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "alice" || body["password"] != "secret" {
			t.Errorf("unexpected credentials: %+v", body)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-456",
			"expires_in":    3600,
			"user_id":       "u1",
			"namespace":     "default",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	pair, err := c.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Login error = %v", err)
	}
	if pair.AccessToken != "access-123" {
		t.Errorf("AccessToken = %s", pair.AccessToken)
	}
	if pair.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %s", pair.RefreshToken)
	}
	if pair.ExpiresIn != 3600 {
		t.Errorf("ExpiresIn = %d, want 3600", pair.ExpiresIn)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusUnauthorized, map[string]string{"error": "invalid"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	_, err := c.Login(context.Background(), "alice", "wrong")
	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401: %v", err)
	}
}

func TestRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/refresh" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] != "old-refresh" {
			t.Errorf("refresh_token = %q", body["refresh_token"])
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    7200,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	pair, err := c.RefreshToken(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken error = %v", err)
	}
	if pair.AccessToken != "new-access" || pair.RefreshToken != "new-refresh" {
		t.Errorf("unexpected pair: %+v", pair)
	}
}

func TestMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/me" {
			t.Errorf("path = %s", r.URL.Path)
		}
		jsonResp(w, http.StatusOK, map[string]any{
			"user_id":   "u1",
			"namespace": "default",
			"roles":     []string{"user", "admin"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me error = %v", err)
	}
	if me.UserID != "u1" {
		t.Errorf("UserID = %s", me.UserID)
	}
	if me.Namespace != "default" {
		t.Errorf("Namespace = %s", me.Namespace)
	}
	if len(me.Roles) != 2 {
		t.Errorf("Roles len = %d", len(me.Roles))
	}
}

// Login 后应自动设置 token（下次请求带 Authorization 头）
func TestLogin_SetsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, http.StatusOK, map[string]any{
			"access_token":  "tok-abc",
			"refresh_token": "ref-xyz",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "default")
	_, err := c.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Login error = %v", err)
	}
	if c.token != "tok-abc" {
		t.Errorf("client.token = %q, want tok-abc（Login 后应自动设置）", c.token)
	}
}
