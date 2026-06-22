package storage

import (
	"bytes"
	"io"
	"testing"
)

func TestCountingReader(t *testing.T) {
	data := []byte("hello s3 counting reader")
	cw := &countingReader{r: bytes.NewReader(data)}

	got, err := io.ReadAll(cw)
	if err != nil {
		t.Fatalf("ReadAll error = %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data mismatch: got %s, want %s", got, data)
	}
	if cw.n != int64(len(data)) {
		t.Errorf("countingReader.n = %d, want %d", cw.n, len(data))
	}
}
