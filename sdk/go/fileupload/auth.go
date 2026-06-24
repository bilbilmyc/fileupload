package fileupload

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// TokenPair 登录/刷新响应：access + refresh token 对。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// UserInfo 当前登录用户信息（/v1/auth/me 响应）。
type UserInfo struct {
	UserID    string   `json:"user_id"`
	Namespace string   `json:"namespace"`
	Roles     []string `json:"roles"`
}

// StatusError 非 200 响应时返回的错误，含 HTTP 状态码与响应体。
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return "fileupload: HTTP " + strconv.Itoa(e.Code) + ": " + e.Body
}

// Login 用 username/password 登录。成功后自动设置 client.token（后续请求带 Authorization）。
func (c *Client) Login(ctx context.Context, username, password string) (*TokenPair, error) {
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.url("/v1/auth/login"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	var pair TokenPair
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		return nil, err
	}
	c.token = pair.AccessToken
	return &pair, nil
}

// RefreshToken 用 refresh token 刷新 access token。
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req, err := http.NewRequestWithContext(ctx, "POST", c.url("/v1/auth/refresh"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	var pair TokenPair
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		return nil, err
	}
	c.token = pair.AccessToken
	return &pair, nil
}

// Me 获取当前登录用户信息（需要已设置 token）。
func (c *Client) Me(ctx context.Context) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url("/v1/auth/me"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Code: resp.StatusCode, Body: readBody(resp)}
	}
	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}