package hasher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestSHA256Hasher_Sum(t *testing.T) {
	h := NewSHA256Hasher()
	ctx := context.Background()

	tests := []struct {
		name string
		data string
	}{
		{"空数据", ""},
		{"短文本", "hello world"},
		{"中文", "你好，世界！"},
		{"多行", "line1\nline2\nline3\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.data)
			hash, n, err := h.Sum(ctx, r)
			if err != nil {
				t.Fatalf("Sum() error = %v", err)
			}
			if n != int64(len(tt.data)) {
				t.Errorf("Sum() n = %d, want %d", n, len(tt.data))
			}
			// 验证哈希正确性
			expected := sha256.Sum256([]byte(tt.data))
			expectedHex := hex.EncodeToString(expected[:])
			if hash != expectedHex {
				t.Errorf("Sum() hash = %s, want %s", hash, expectedHex)
			}
		})
	}
}

func TestSHA256Hasher_Sum_Large(t *testing.T) {
	h := NewSHA256Hasher()
	ctx := context.Background()

	// 1MB 随机数据
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 256)
	}

	r := bytes.NewReader(data)
	hash, n, err := h.Sum(ctx, r)
	if err != nil {
		t.Fatalf("Sum() error = %v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("Sum() n = %d, want %d", n, len(data))
	}

	expected := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expected[:])
	if hash != expectedHex {
		t.Errorf("Sum() hash mismatch")
	}
}

func TestSHA256Hasher_TeeReader(t *testing.T) {
	h := NewSHA256Hasher()
	data := []byte("tee reader test data, 分片校验场景验证")

	teeReader, acc := h.TeeReader(bytes.NewReader(data))

	// 读取全部数据
	read, err := io.ReadAll(teeReader)
	if err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if !bytes.Equal(read, data) {
		t.Errorf("TeeReader 读取数据不匹配")
	}

	// 验证累计哈希
	expected := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expected[:])
	if acc.SumHex() != expectedHex {
		t.Errorf("Accumulator SumHex() = %s, want %s", acc.SumHex(), expectedHex)
	}
	if acc.N() != int64(len(data)) {
		t.Errorf("Accumulator N() = %d, want %d", acc.N(), len(data))
	}
}

func TestSHA256Hasher_TeeReader_PartialRead(t *testing.T) {
	h := NewSHA256Hasher()
	data := []byte("partial read test data for accumulator")

	teeReader, acc := h.TeeReader(bytes.NewReader(data))

	// 只读前 10 个字节
	buf := make([]byte, 10)
	n, err := teeReader.Read(buf)
	if err != nil {
		t.Fatalf("Read error = %v", err)
	}
	if n != 10 {
		t.Errorf("Read n = %d, want 10", n)
	}

	// 此时累计器应该只有前 10 字节的哈希
	partialHash := sha256.Sum256(data[:10])
	expectedHex := hex.EncodeToString(partialHash[:])
	if acc.SumHex() != expectedHex {
		t.Errorf("Partial SumHex() = %s, want %s", acc.SumHex(), expectedHex)
	}
	if acc.N() != 10 {
		t.Errorf("N() = %d, want 10", acc.N())
	}
}

func TestNewAccumulator_Write(t *testing.T) {
	a := NewAccumulator()

	data := []byte("直接写累计器测试")
	n, err := a.Write(data)
	if err != nil {
		t.Fatalf("Write error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write n = %d, want %d", n, len(data))
	}

	expected := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expected[:])
	if a.SumHex() != expectedHex {
		t.Errorf("SumHex() = %s, want %s", a.SumHex(), expectedHex)
	}
	if a.N() != int64(len(data)) {
		t.Errorf("N() = %d, want %d", a.N(), len(data))
	}
}

func TestNewAccumulator_Empty(t *testing.T) {
	a := NewAccumulator()
	// 空累计器
	emptyHash := sha256.Sum256([]byte{})
	if a.SumHex() != hex.EncodeToString(emptyHash[:]) {
		t.Error("空累计器 SumHex 应该返回空字符串的 SHA-256")
	}
	if a.N() != 0 {
		t.Errorf("空累计器 N() = %d, want 0", a.N())
	}
}
