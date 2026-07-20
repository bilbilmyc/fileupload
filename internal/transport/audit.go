package transport

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// auditLogger 是传输层写入审计事件所需的最小端口。审计写入失败不得阻塞文件传输。
type auditLogger interface {
	WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error
}

type auditRequestState struct {
	userID    string
	namespace string
}

type auditDetail struct {
	Method    string `json:"method"`
	Route     string `json:"route,omitempty"`
	Status    int    `json:"status"`
	RequestID string `json:"request_id,omitempty"`
	RemoteIP  string `json:"remote_ip,omitempty"`
}

func writeAudit(logger auditLogger, ctx context.Context, action, targetType, targetID, namespace, detail string) {
	if logger == nil {
		return
	}
	userID := "anonymous"
	if state, ok := ctx.Value(ctxKeyAuditState).(*auditRequestState); ok {
		if state.userID != "" {
			userID = state.userID
		}
		if namespace == "" {
			namespace = state.namespace
		}
	}
	if claims := GetAuthClaims(ctx); claims != nil && claims.UserID != "" {
		userID = claims.UserID
	}
	if namespace == "" {
		namespace = GetNamespace(ctx)
	}
	if namespace == "" {
		namespace = "default"
	}
	if err := logger.WriteAuditLog(ctx, &domain.AuditLogEntry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		UserID:     userID,
		Namespace:  namespace,
		Detail:     detail,
	}); err != nil {
		slog.WarnContext(ctx, "write audit log failed", "action", action, "error", err)
	}
}

func updateAuditActor(ctx context.Context, userID string) {
	if state, ok := ctx.Value(ctxKeyAuditState).(*auditRequestState); ok && userID != "" {
		state.userID = userID
	}
}

func updateAuditNamespace(ctx context.Context, namespace string) {
	if state, ok := ctx.Value(ctxKeyAuditState).(*auditRequestState); ok && namespace != "" {
		state.namespace = namespace
	}
}

// Audit 记录安全敏感和会改变系统状态的 HTTP 操作。下载与公开分享由对应
// handler 记录更精确的语义事件，因此这里有意跳过这些路由，避免重复记录。
func (m *Middleware) Audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.audit == nil {
			next.ServeHTTP(w, r)
			return
		}

		state := &auditRequestState{}
		ctx := context.WithValue(r.Context(), ctxKeyAuditState, state)
		r = r.WithContext(ctx)
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		action, targetType, targetID, ok := classifyAuditEvent(r, rw.status)
		if !ok {
			return
		}
		detail, err := json.Marshal(auditDetail{
			Method:    r.Method,
			Route:     r.Pattern,
			Status:    rw.status,
			RequestID: GetRequestID(r.Context()),
			RemoteIP:  auditRemoteIP(r.RemoteAddr),
		})
		if err != nil {
			detail = []byte(`{"status":0}`)
		}
		writeAudit(m.audit, r.Context(), action, targetType, targetID, state.namespace, string(detail))
	})
}

func classifyAuditEvent(r *http.Request, status int) (action, targetType, targetID string, ok bool) {
	r.Pattern = strings.TrimPrefix(r.Pattern, r.Method+" ")
	if r.Pattern == "" {
		r.Pattern = canonicalAuditRoute(r.Method, r.URL.Path)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "access_denied", "http", "", true
	}
	if status == http.StatusTooManyRequests {
		return "rate_limited", "http", "", true
	}

	switch r.Method + " " + r.Pattern {
	case "POST /v1/auth/login":
		return "login", "user", "", true
	case "POST /v1/auth/refresh":
		return "token_refresh", "user", "", true
	case "POST /uploads":
		return "upload_create", "session", "", true
	case "DELETE /uploads/{id}":
		return "upload_cancel", "session", auditPathValue(r, "id"), true
	case "POST /v1/uploads/init":
		return "upload_init", "session", "", true
	case "POST /v1/uploads/{id}/finalize":
		return "upload_finalize", "session", auditPathValue(r, "id"), true
	case "POST /v1/dirs":
		return "directory_submit", "directory", "", true
	case "PATCH /v1/files/{id}":
		return "rename", "file", auditPathValue(r, "id"), true
	case "DELETE /v1/files/{id}":
		return "delete", "file", auditPathValue(r, "id"), true
	case "DELETE /v1/dirs/{id}":
		return "delete", "directory", auditPathValue(r, "id"), true
	case "POST /v1/trash/{id}/restore":
		return "restore", "file", auditPathValue(r, "id"), true
	case "DELETE /v1/trash/{id}":
		return "purge", "file", auditPathValue(r, "id"), true
	case "HEAD /v1/files":
		// 404 是正常的秒传未命中，不应淹没有价值的审计事件。
		if status == http.StatusOK {
			return "instant_upload", "file", "", true
		}
	case "POST /v1/batch/{action}":
		action := strings.TrimSpace(auditPathValue(r, "action"))
		if action != "" {
			return "batch_" + action, "batch", "", true
		}
	case "POST /v1/admin/scan":
		return "consistency_scan", "system", "", true
	case "GET /v1/admin/audit":
		return "audit_read", "system", "", true
	case "GET /v1/admin/status":
		return "admin_status_read", "system", "", true
	case "GET /v1/shares":
		return "share_list", "share", "", true
	}
	return "", "", "", false
}

func canonicalAuditRoute(method, path string) string {
	switch path {
	case "/v1/auth/login", "/v1/auth/refresh", "/uploads", "/v1/uploads/init",
		"/v1/dirs", "/v1/files", "/v1/admin/scan", "/v1/admin/audit",
		"/v1/admin/status", "/v1/shares":
		return path
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "uploads":
		return "/uploads/{id}"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "uploads" && parts[3] == "finalize":
		return "/v1/uploads/{id}/finalize"
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "files":
		return "/v1/files/{id}"
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "dirs":
		return "/v1/dirs/{id}"
	case len(parts) == 4 && parts[0] == "v1" && parts[1] == "trash" && parts[3] == "restore":
		return "/v1/trash/{id}/restore"
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "trash":
		return "/v1/trash/{id}"
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "batch":
		return "/v1/batch/{action}"
	case len(parts) == 3 && parts[0] == "v1" && parts[1] == "shares":
		return "/v1/shares/{token}"
	case len(parts) == 2 && parts[0] == "s":
		return "/s/{token}"
	}
	_ = method
	return ""
}

func auditPathValue(r *http.Request, name string) string {
	if value := r.PathValue(name); value != "" {
		return value
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch name {
	case "id":
		if len(parts) >= 2 && parts[0] == "uploads" {
			return parts[1]
		}
		if len(parts) >= 3 && parts[0] == "v1" {
			return parts[2]
		}
	case "action":
		if len(parts) == 3 && parts[0] == "v1" && parts[1] == "batch" {
			return parts[2]
		}
	}
	return ""
}

func auditRemoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}
