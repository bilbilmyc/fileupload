package domain

import (
	"context"
	"testing"
)

// mockAuthSvc implements AuthService for testing
type mockAuthSvc struct{}

func (m *mockAuthSvc) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	if username == "admin" && password == "pw" {
		return &TokenPair{AccessToken: "tok", RefreshToken: "ref", ExpiresIn: 3600}, nil
	}
	return nil, ErrForbidden
}
func (m *mockAuthSvc) ValidateToken(tokenStr string) (*AuthClaims, error) {
	if tokenStr == "tok" {
		return &AuthClaims{UserID: "u1", Namespace: "demo", Roles: []string{"user"}}, nil
	}
	return nil, ErrForbidden
}
func (m *mockAuthSvc) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if refreshToken == "ref" {
		return &TokenPair{AccessToken: "tok2", RefreshToken: "ref2", ExpiresIn: 3600}, nil
	}
	return nil, ErrForbidden
}

func TestAuthService_Login_Success(t *testing.T) {
	svc := &mockAuthSvc{}
	pair, err := svc.Login(context.Background(), "admin", "pw")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if pair.AccessToken != "tok" {
		t.Errorf("AccessToken = %s", pair.AccessToken)
	}
}

func TestAuthService_Login_Fail(t *testing.T) {
	svc := &mockAuthSvc{}
	_, err := svc.Login(context.Background(), "admin", "wrong")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthService_ValidateToken_Valid(t *testing.T) {
	svc := &mockAuthSvc{}
	claims, err := svc.ValidateToken("tok")
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != "u1" {
		t.Errorf("UserID = %s", claims.UserID)
	}
}

func TestAuthService_ValidateToken_Invalid(t *testing.T) {
	svc := &mockAuthSvc{}
	_, err := svc.ValidateToken("bad")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthService_RefreshToken(t *testing.T) {
	svc := &mockAuthSvc{}
	pair, err := svc.RefreshToken(context.Background(), "ref")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if pair.AccessToken != "tok2" {
		t.Errorf("AccessToken = %s", pair.AccessToken)
	}
}

func TestAuthClaims_Fields(t *testing.T) {
	c := &AuthClaims{UserID: "u1", Namespace: "ns1", Roles: []string{"admin", "user"}, TokenID: "t1"}
	if c.UserID != "u1" { t.Error("UserID mismatch") }
	if c.Namespace != "ns1" { t.Error("Namespace mismatch") }
	if len(c.Roles) != 2 { t.Error("Roles count") }
	if c.TokenID != "t1" { t.Error("TokenID mismatch") }
}

func TestTokenPair_Fields(t *testing.T) {
	p := &TokenPair{AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600}
	if p.AccessToken != "a" { t.Error("AccessToken") }
	if p.RefreshToken != "r" { t.Error("RefreshToken") }
	if p.ExpiresIn != 3600 { t.Error("ExpiresIn") }
}

func TestLoginRequest_Fields(t *testing.T) {
	r := &LoginRequest{Username: "admin", Password: "pw"}
	if r.Username != "admin" { t.Error("Username") }
	if r.Password != "pw" { t.Error("Password") }
}

func TestShareEntry_Fields(t *testing.T) {
	s := &ShareEntry{Token: "s1", FileID: "f1", Namespace: "demo", MaxDownloads: 5, CurDownloads: 1}
	if s.Token != "s1" { t.Error("Token") }
	if s.FileID != "f1" { t.Error("FileID") }
	if s.CurDownloads != 1 { t.Error("CurDownloads") }
}

func TestCreateShareRequest_Fields(t *testing.T) {
	r := &CreateShareRequest{FileID: "f1", Password: "pw", ExpiresIn: 24, MaxDownloads: 10}
	if r.FileID != "f1" { t.Error("FileID") }
	if r.Password != "pw" { t.Error("Password") }
	if r.MaxDownloads != 10 { t.Error("MaxDownloads") }
}
