package domain

import (
	"testing"
)

func TestAuditLogEntry_Fields(t *testing.T) {
	e := &AuditLogEntry{
		ID: 1, Action: "delete", TargetType: "file",
		TargetID: "f1", UserID: "u1", Namespace: "demo", Detail: "deleted file",
	}
	if e.Action != "delete" {
		t.Errorf("Action = %s", e.Action)
	}
	if e.UserID != "u1" {
		t.Errorf("UserID = %s", e.UserID)
	}
}

func TestSystemStatus_Defaults(t *testing.T) {
	s := &SystemStatus{}
	if s.WorkerPool != nil {
		t.Error("WorkerPool should be nil")
	}
	if s.Storage != nil {
		t.Error("Storage should be nil")
	}
}

func TestAuditLogPage_Empty(t *testing.T) {
	p := &AuditLogPage{Entries: []*AuditLogEntry{}, Total: 0, Page: 1, PerPage: 50}
	if len(p.Entries) != 0 {
		t.Error("expected empty entries")
	}
	if p.Total != 0 {
		t.Errorf("Total = %d", p.Total)
	}
}
