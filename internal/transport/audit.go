package transport

import (
	"context"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// auditLogger 是传输层写入审计事件所需的最小端口。审计写入失败不得阻塞文件传输。
type auditLogger interface {
	WriteAuditLog(ctx context.Context, entry *domain.AuditLogEntry) error
}

func writeAudit(logger auditLogger, ctx context.Context, action, targetType, targetID, namespace, detail string) {
	if logger == nil {
		return
	}
	userID := "anonymous"
	if claims := GetAuthClaims(ctx); claims != nil && claims.UserID != "" {
		userID = claims.UserID
	}
	_ = logger.WriteAuditLog(ctx, &domain.AuditLogEntry{
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		UserID:     userID,
		Namespace:  namespace,
		Detail:     detail,
	})
}
