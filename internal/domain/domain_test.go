package domain

import (
	"strings"
	"testing"
)

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewID()
		if ids[id] {
			t.Fatalf("重复 ID: %s", id)
		}
		ids[id] = true
	}
	_ = ids
}

func TestNewID_Format(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := NewID()
		if len(id) < 8 {
			t.Errorf("ID 太短: %s (len=%d)", id, len(id))
		}
	}
}

func TestDownloadRange_IsZero(t *testing.T) {
	tests := []struct {
		name   string
		rng    DownloadRange
		isZero bool
	}{
		{"未请求范围", DownloadRange{}, true},
		{"旧调用方带 offset", DownloadRange{Offset: 100}, false},
		{"旧调用方带 length", DownloadRange{Length: 1024}, false},
		{"从零开始的请求范围", DownloadRange{Offset: 0, Requested: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rng.IsZero(); got != tt.isZero {
				t.Errorf("IsZero() = %v, want %v", got, tt.isZero)
			}
		})
	}
}

func TestDomainError(t *testing.T) {
	err := DomainError("测试错误")
	if err.Error() != "测试错误" {
		t.Errorf("DomainError.Error() = %s", err.Error())
	}
}

func TestErrorConstants(t *testing.T) {
	errors := []error{
		ErrSliceChecksum,
		ErrContentChecksum,
		ErrSessionNotFound,
		ErrSessionState,
		ErrOffsetConflict,
		ErrForbidden,
		ErrBusy,
		ErrStorage,
		ErrCorrupted,
		ErrNotFound,
		ErrInvalidArgument,
		ErrPathTraversal,
	}
	for _, err := range errors {
		if err == nil {
			t.Error("错误常量为 nil")
		}
		if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("错误 %v 的消息为空", err)
		}
	}
}

func TestSafeStorageName(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"普通英文", "report.pdf"},
		{"空格", "my report v2.pdf"},
		{"中文", "中文文档.txt"},
		{"特殊字符无修改", "hello_world.tar.gz"},
		{"多个点", "a.b.c.d.tar.gz"},
		{"路径穿越", "../../etc/passwd"},
		{"空字符串", ""},
		{"点", "."},
		{"双点", ".."},
		{"Windows 保留字符冒号", "file:name.txt"},
		{"Windows 保留字符引号", `file"name".txt`},
		{"Windows 保留字符尖括号", "a<b>c.txt"},
		{"Windows 保留字符管道", "a|b.txt"},
		{"Windows 保留设备名", "CON.txt"},
		{"Windows 保留设备名大写", "NUL.txt"},
		{"尾部点", "file.."},
		{"尾部空格", "file  "},
		{"纯尾部点空格", "..  .."},
		{"只有点", "..."},
		{"前导空格", "  leading.txt"},
		{"长文件名无扩展", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"长文件名有扩展", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.txt"},
		{"混合", "我的  2024 Report (FINAL).tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := safeStorageName(tt.in)

			if result == "" {
				t.Error("safeStorageName 返回空字符串")
			}

			// 不应包含路径分隔符
			if contains(result, '/') || contains(result, '\\') {
				t.Errorf("结果包含路径分隔符: %s", result)
			}

			// 不应包含 null 字节
			if contains(result, '\x00') {
				t.Errorf("结果包含 null 字节: %s", result)
			}

			// 长度不超过 200 字节
			if len([]byte(result)) > 200 {
				t.Errorf("结果过长: %d 字节", len([]byte(result)))
			}

			// 尾部不应有点或空格
			if len(result) > 0 {
				last := result[len(result)-1]
				if last == '.' || last == ' ' {
					t.Errorf("结果尾部含点或空格: %q", result)
				}
			}
		})
	}
}

func contains(s string, ch byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ch {
			return true
		}
	}
	return false
}

func TestSessionStatus(t *testing.T) {
	if SessionActive != "active" {
		t.Errorf("SessionActive = %s", SessionActive)
	}
	if SessionFinalizing != "finalizing" {
		t.Errorf("SessionFinalizing = %s", SessionFinalizing)
	}
	if SessionCompleted != "completed" {
		t.Errorf("SessionCompleted = %s", SessionCompleted)
	}
	if SessionAborted != "aborted" {
		t.Errorf("SessionAborted = %s", SessionAborted)
	}
}
