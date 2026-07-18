package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

func TestJWTService_Login_Success(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	pair, err := svc.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if pair.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if pair.RefreshToken == "" {
		t.Error("RefreshToken is empty")
	}
	if pair.ExpiresIn <= 0 {
		t.Errorf("ExpiresIn = %d, want > 0", pair.ExpiresIn)
	}
}

func TestJWTService_Login_WrongPassword(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	_, err := svc.Login(context.Background(), "admin", "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestJWTService_Login_UnknownUser(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	_, err := svc.Login(context.Background(), "nobody", "pw")
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestJWTService_ValidateToken_Valid(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	pair, _ := svc.Login(context.Background(), "admin", "admin123")
	claims, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != "u-admin" {
		t.Errorf("UserID = %s, want u-admin", claims.UserID)
	}
	if claims.Namespace != "default" {
		t.Errorf("Namespace = %s, want default", claims.Namespace)
	}
	if len(claims.Roles) == 0 {
		t.Error("Roles is empty")
	}
}

func TestJWTService_ValidateToken_RejectsRefreshToken(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())
	pair, err := svc.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if _, err := svc.ValidateToken(pair.RefreshToken); err == nil {
		t.Fatal("expected refresh token to be rejected by access-token validation")
	}
}

func TestJWTService_ValidateToken_Invalid(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	_, err := svc.ValidateToken("invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestJWTService_ValidateToken_WrongKey(t *testing.T) {
	svc1 := NewJWTService("secret1", time.Hour, DevelopmentUsers())
	svc2 := NewJWTService("secret2", time.Hour, DevelopmentUsers())

	pair, _ := svc1.Login(context.Background(), "admin", "admin123")
	_, err := svc2.ValidateToken(pair.AccessToken)
	if err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestJWTService_RefreshToken(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	original, _ := svc.Login(context.Background(), "admin", "admin123")

	refreshed, err := svc.RefreshToken(context.Background(), original.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if refreshed.AccessToken == "" {
		t.Error("refreshed AccessToken is empty")
	}
	if refreshed.RefreshToken == "" {
		t.Error("refreshed RefreshToken is empty")
	}
	if refreshed.AccessToken == original.AccessToken {
		t.Error("refreshed token should be different from original")
	}
}

func TestJWTService_RefreshToken_RejectsReplay(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())
	original, err := svc.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if _, err := svc.RefreshToken(context.Background(), original.RefreshToken); err != nil {
		t.Fatalf("first RefreshToken failed: %v", err)
	}
	if _, err := svc.RefreshToken(context.Background(), original.RefreshToken); err == nil {
		t.Fatal("expected replayed refresh token to be rejected")
	}
}

func TestJWTService_RefreshToken_Invalid(t *testing.T) {
	svc := NewJWTService("test-secret", time.Hour, DevelopmentUsers())

	_, err := svc.RefreshToken(context.Background(), "invalid")
	if err == nil {
		t.Fatal("expected error for invalid refresh token")
	}
}

func TestJWTService_CustomUser(t *testing.T) {
	users := []domain.AuthUser{
		{ID: "u-custom", Username: "custom", Password: "pass", Namespace: "my-ns", Roles: []string{"user"}},
	}
	svc := NewJWTService("test-secret", time.Hour, users)

	pair, err := svc.Login(context.Background(), "custom", "pass")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	claims, _ := svc.ValidateToken(pair.AccessToken)
	if claims.Namespace != "my-ns" {
		t.Errorf("Namespace = %s, want my-ns", claims.Namespace)
	}
}

func TestJWTService_TokenExpiry(t *testing.T) {
	// 使用极短的过期时间（1秒），然后等待过期
	svc := NewJWTService("test-secret", time.Second, DevelopmentUsers())
	pair, _ := svc.Login(context.Background(), "admin", "admin123")

	// 立刻验证应该成功
	_, err := svc.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("expected token valid immediately: %v", err)
	}
}

type fakeRefreshStore struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func (s *fakeRefreshStore) ClaimRefreshToken(_ context.Context, tokenID string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.ids[tokenID]; exists {
		return false, nil
	}
	s.ids[tokenID] = struct{}{}
	return true, nil
}

func TestJWTService_RefreshToken_SharedStoreRejectsAcrossInstances(t *testing.T) {
	store := &fakeRefreshStore{ids: make(map[string]struct{})}
	users := DevelopmentUsers()
	first := NewJWTServiceWithRefreshStore("test-secret", time.Hour, users, store)
	second := NewJWTServiceWithRefreshStore("test-secret", time.Hour, users, store)

	pair, err := first.Login(context.Background(), "admin", "admin123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if _, err := first.RefreshToken(context.Background(), pair.RefreshToken); err != nil {
		t.Fatalf("first RefreshToken failed: %v", err)
	}
	if _, err := second.RefreshToken(context.Background(), pair.RefreshToken); err == nil {
		t.Fatal("expected replayed refresh token to be rejected across instances")
	}
}
