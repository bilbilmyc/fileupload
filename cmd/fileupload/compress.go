package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// sha256Sum 计算字节片的 SHA-256 十六进制字符串。
func sha256Sum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// compressBuffer 使用指定格式压缩数据，返回压缩后的字节。
func compressBuffer(data []byte, format string) ([]byte, error) {
	switch format {
	case "zstd":
		var buf bytes.Buffer
		enc, err := zstd.NewWriter(&buf)
		if err != nil {
			return nil, err
		}
		if _, err := enc.Write(data); err != nil {
			enc.Close()
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "none", "":
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported compress format: %s", format)
	}
}

// decompressReader 返回一个解压 reader，包装给定的压缩数据流。
func decompressReader(r io.Reader, format string) (io.Reader, error) {
	switch format {
	case "zstd":
		return zstd.NewReader(r)
	case "none", "":
		return r, nil
	default:
		return nil, fmt.Errorf("unsupported decompress format: %s", format)
	}
}