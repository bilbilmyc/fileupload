package transport

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

const (
	shareAccessCookieName = "fileupload_share_access"
	shareAccessCookieTTL  = 15 * time.Minute
)

// ShareHandler 分享链接 HTTP 处理器。
// passwordKey 是进程内签名密钥，用于签发短期、HttpOnly 的密码验证 cookie；服务重启后
// 旧 cookie 会自动失效并要求重新输入密码。
type ShareHandler struct {
	shareSvc        *domain.ShareService
	downloadSvc     *domain.DownloadService
	passwordKey     []byte
	passwordLimiter *sharePasswordLimiter
	audit           auditLogger
}

// NewShareHandler 创建分享处理器。
func NewShareHandler(shareSvc *domain.ShareService, downloadSvc *domain.DownloadService) *ShareHandler {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// 无法获得安全随机密钥时不签发密码访问 cookie；请求会安全地要求重试，
		// 而不是退化为可预测的签名密钥。
		key = nil
	}
	return &ShareHandler{
		shareSvc:        shareSvc,
		downloadSvc:     downloadSvc,
		passwordKey:     key,
		passwordLimiter: newSharePasswordLimiter(defaultSharePasswordMaxFailures, defaultSharePasswordCooldown, nil),
	}
}

// withPasswordLimiter 替换公开分享密码限流器，仅用于注入确定性测试策略。
func (h *ShareHandler) withPasswordLimiter(limiter *sharePasswordLimiter) *ShareHandler {
	h.passwordLimiter = limiter
	return h
}

// WithAuditLogger 为分享创建、撤销和公开下载补充非阻塞审计记录。
func (h *ShareHandler) WithAuditLogger(logger auditLogger) *ShareHandler {
	h.audit = logger
	return h
}

// CreateShare POST /v1/share
func (h *ShareHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	entry, err := h.shareSvc.CreateShare(r.Context(), req, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	writeAudit(h.audit, r.Context(), "share_create", "file", entry.FileID, namespace, "created file share link")
	respondJSON(w, http.StatusCreated, entry)
}

// ListShares GET /v1/shares?file_id=... — 查询当前空间的可管理分享链接。
func (h *ShareHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	namespace := GetNamespace(r.Context())
	entries, err := h.shareSvc.ListShares(r.Context(), namespace, r.URL.Query().Get("file_id"))
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"shares": entries})
}

// RevokeShare DELETE /v1/shares/{token} — 立即撤销分享链接。
func (h *ShareHandler) RevokeShare(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())
	if err := h.shareSvc.RevokeShare(r.Context(), token, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	// 分享 token 本身是凭据，不写入审计明细，避免日志成为泄露渠道。
	writeAudit(h.audit, r.Context(), "share_revoke", "share", "", namespace, "revoked file share link")
	w.WriteHeader(http.StatusNoContent)
}

// SubmitSharePassword POST /s/{token} — 验证浏览器密码表单并签发短期访问 cookie。
func (h *ShareHandler) SubmitSharePassword(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if len(h.passwordKey) == 0 {
		h.renderSharePasswordPage(w, token, "服务暂时无法建立安全访问会话，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderSharePasswordPage(w, token, "请求格式无效，请重试。", http.StatusBadRequest)
		return
	}
	client := shareClientAddress(r)
	if retryAfter, allowed := h.passwordLimiter.Allow(token, client); !allowed {
		h.respondSharePasswordRateLimited(w, r, token, retryAfter)
		return
	}
	if _, err := h.shareSvc.AuthorizeShare(r.Context(), token, r.PostForm.Get("password")); err != nil {
		if err == domain.ErrForbidden {
			if retryAfter, locked := h.passwordLimiter.RecordFailure(token, client); locked {
				h.auditSharePasswordThrottled(r, retryAfter)
				h.respondSharePasswordRateLimited(w, r, token, retryAfter)
				return
			}
			writeAudit(h.audit, r.Context(), "share_password_failed", "share", "", "public", "rejected password for public share")
			h.renderSharePasswordPage(w, token, "访问密码不正确，请重新输入。", http.StatusUnauthorized)
			return
		}
		h.respondShareAccessError(w, r, err)
		return
	}

	h.passwordLimiter.Reset(token, client)
	h.setShareAccessCookie(w, r, token)
	http.Redirect(w, r, "/s/"+token, http.StatusSeeOther)
}

// AccessShare GET /s/{token}
func (h *ShareHandler) AccessShare(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		// 兼容直接调用 handler 的旧测试与非路由调用。
		token = strings.TrimPrefix(r.URL.Path, "/s/")
	}
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	var (
		entry *domain.ShareEntry
		err   error
	)
	hasCookie := h.hasValidShareAccessCookie(r, token)
	password := r.Header.Get("X-Share-Password")
	client := shareClientAddress(r)

	// 浏览器首次打开受保护链接时，仅检查分享状态并展示密码页，避免空密码触发 bcrypt 校验。
	if !hasCookie && password == "" && acceptsHTML(r) {
		entry, err = h.shareSvc.InspectShare(r.Context(), token)
		if err != nil {
			h.respondShareAccessError(w, r, err)
			return
		}
		if entry.PasswordProtected {
			h.renderSharePasswordPage(w, token, "", http.StatusUnauthorized)
			return
		}
	}

	if hasCookie {
		entry, err = h.shareSvc.AccessAuthorizedShare(r.Context(), token)
	} else {
		if password != "" {
			if retryAfter, allowed := h.passwordLimiter.Allow(token, client); !allowed {
				h.auditSharePasswordThrottled(r, retryAfter)
				h.respondSharePasswordRateLimited(w, r, token, retryAfter)
				return
			}
		}
		entry, err = h.shareSvc.AccessShare(r.Context(), token, password)
	}
	if err != nil {
		if err == domain.ErrForbidden && password != "" {
			if retryAfter, locked := h.passwordLimiter.RecordFailure(token, client); locked {
				h.auditSharePasswordThrottled(r, retryAfter)
				h.respondSharePasswordRateLimited(w, r, token, retryAfter)
				return
			}
			writeAudit(h.audit, r.Context(), "share_password_failed", "share", "", "public", "rejected password for public share")
		}
		if err == domain.ErrForbidden && acceptsHTML(r) {
			h.renderSharePasswordPage(w, token, "", http.StatusUnauthorized)
			return
		}
		h.respondShareAccessError(w, r, err)
		return
	}
	if password != "" {
		h.passwordLimiter.Reset(token, client)
	}

	// 测试可不注入下载服务，只验证分享访问流程；生产环境直接流式输出文件，
	// 公开链接无需访问受鉴权保护的 /v1/files 接口。
	if h.downloadSvc == nil {
		http.Redirect(w, r, "/v1/files/"+entry.FileID+"?namespace="+entry.Namespace, http.StatusFound)
		return
	}

	rng, err := parseRangeHeader(r.Header.Get("Range"))
	if err != nil {
		w.Header().Set("Content-Range", "bytes */*")
		respondError(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}
	fileReader, err := h.downloadSvc.GetFile(r.Context(), entry.FileID, entry.Namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()
	writeAudit(h.audit, r.Context(), "share_download", "file", entry.FileID, entry.Namespace, "downloaded through public share link")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-SHA256", fileReader.Blob.SHA256)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", fileReader.File.Name))
	w.Header().Set("Cache-Control", "private, no-store")
	if fileReader.Range.Requested {
		w.Header().Set("Content-Range", formatContentRange(fileReader.Range.Offset, fileReader.Range.Length, fileReader.FileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusOK)
	}
	_, _ = io.Copy(w, fileReader.Reader)
}

func (h *ShareHandler) auditSharePasswordThrottled(r *http.Request, retryAfter time.Duration) {
	detail := fmt.Sprintf("password verification temporarily throttled; retry after %d seconds", int((retryAfter+time.Second-1)/time.Second))
	writeAudit(h.audit, r.Context(), "share_password_throttled", "share", "", "public", detail)
}

func (h *ShareHandler) respondSharePasswordRateLimited(w http.ResponseWriter, r *http.Request, token string, retryAfter time.Duration) {
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	message := fmt.Sprintf("密码尝试次数过多，请在 %d 秒后再试。", seconds)
	if acceptsHTML(r) {
		h.renderSharePasswordPage(w, token, message, http.StatusTooManyRequests)
		return
	}
	respondJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               "密码尝试次数过多，请稍后再试",
		"code":                "share_password_rate_limited",
		"retry_after_seconds": seconds,
	})
}

func (h *ShareHandler) respondShareAccessError(w http.ResponseWriter, r *http.Request, err error) {
	if err == domain.ErrForbidden {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码错误", "code": "share_password_required"})
		return
	}
	if acceptsHTML(r) {
		h.renderShareUnavailablePage(w, domainErrorToStatus(err), shareUnavailableMessage(err))
		return
	}
	respondError(w, domainErrorToStatus(err), err)
}

func shareUnavailableMessage(err error) string {
	if err == domain.ErrShareExhausted {
		return "该分享链接已过期，或允许的下载次数已用尽。"
	}
	return "该分享链接不存在，或已被创建者撤销。"
}

func acceptsHTML(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func (h *ShareHandler) setShareAccessCookie(w http.ResponseWriter, r *http.Request, token string) {
	expiresAt := time.Now().Add(shareAccessCookieTTL).Unix()
	signature := h.signShareAccess(token, expiresAt)
	http.SetCookie(w, &http.Cookie{
		Name:     shareAccessCookieName,
		Value:    strconv.FormatInt(expiresAt, 10) + "." + hex.EncodeToString(signature),
		Path:     "/s/" + token,
		Expires:  time.Unix(expiresAt, 0),
		MaxAge:   int(shareAccessCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *ShareHandler) hasValidShareAccessCookie(r *http.Request, token string) bool {
	if len(h.passwordKey) == 0 {
		return false
	}
	cookie, err := r.Cookie(shareAccessCookieName)
	if err != nil {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	expiresAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() >= expiresAt {
		return false
	}
	actual, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return hmac.Equal(actual, h.signShareAccess(token, expiresAt))
}

func (h *ShareHandler) signShareAccess(token string, expiresAt int64) []byte {
	mac := hmac.New(sha256.New, h.passwordKey)
	_, _ = mac.Write([]byte(token))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strconv.FormatInt(expiresAt, 10)))
	return mac.Sum(nil)
}

const sharePageStyle = `body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f3f6fb;color:#172033;font:15px/1.5 Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.card{box-sizing:border-box;width:min(420px,calc(100vw - 32px));padding:32px;background:#fff;border:1px solid #e2eaf5;border-radius:16px;box-shadow:0 16px 42px rgba(32,64,115,.12)}.eyebrow{color:#718097;font-size:11px;font-weight:800;letter-spacing:.12em}.icon{display:grid;width:44px;height:44px;margin:14px 0;place-items:center;border-radius:12px;font-size:22px}.icon--secure{background:#eaf3ff;color:#2d6cdf}.icon--error{background:#fff2ef;color:#d64b35}h1{margin:0;font-size:24px;letter-spacing:-.03em}p{margin:10px 0 22px;color:#68748a}label{display:block;margin-bottom:7px;font-size:13px;font-weight:700}input{box-sizing:border-box;width:100%;padding:11px 12px;border:1px solid #cfd9e8;border-radius:9px;font:inherit}input:focus{outline:3px solid #dcecff;border-color:#2d6cdf}button{width:100%;margin-top:16px;padding:11px 14px;border:0;border-radius:9px;background:#2d6cdf;color:#fff;font:700 14px inherit;cursor:pointer}.notice{padding:9px 11px;background:#fff5f3;border:1px solid #ffd4cc;border-radius:8px;color:#b53820;font-size:13px}`

func (h *ShareHandler) renderSharePasswordPage(w http.ResponseWriter, token, hint string, status int) {
	message := "请输入分享创建者提供的访问密码。"
	if hint != "" {
		message = hint
	}
	form := `<form method="post" action="/s/` + html.EscapeString(token) + `"><label for="password">访问密码</label><input id="password" name="password" type="password" autocomplete="current-password" required autofocus><button type="submit">验证并下载</button></form>`
	renderSharePage(w, status, "受保护的文件分享", "SECURE FILE SHARE", "secure", "↧", "此分享受密码保护", message, passwordPageHint(hint)+form)
}

func (h *ShareHandler) renderShareUnavailablePage(w http.ResponseWriter, status int, message string) {
	renderSharePage(w, status, "分享链接不可用", "FILE SHARE", "error", "!", "此分享链接不可用", message, "")
}

func renderSharePage(w http.ResponseWriter, status int, pageTitle, eyebrow, iconKind, icon, title, message, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>%s</style></head><body><main class="card"><span class="eyebrow">%s</span><div class="icon icon--%s">%s</div><h1>%s</h1><p>%s</p>%s</main></body></html>`, html.EscapeString(pageTitle), sharePageStyle, html.EscapeString(eyebrow), html.EscapeString(iconKind), html.EscapeString(icon), html.EscapeString(title), html.EscapeString(message), body)
}

func passwordPageHint(hint string) string {
	if hint == "" {
		return ""
	}
	return `<div class="notice">` + html.EscapeString(hint) + `</div>`
}
