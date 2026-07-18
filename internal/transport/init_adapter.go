package transport

import (
	"net/http"
	"strconv"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// initSessionInput 是协议无关的上传初始化输入。
// 两种协议（TUS / REST）的 handler 都把协议特定的请求解码成这个结构，
// 再调用 uploadSvc.CreateSession。这样 CreateSession 的 6 个参数语义集中在
// 一处，避免 TUS / REST 各自重复解析逻辑。
type initSessionInput struct {
	uploadLength int64
	sha256       string
	compression  domain.CompressionFormat
	chunkSize    int64
	fileName     string
}

// parseTusInit 解析 tus.io 协议的上传初始化请求。
//
// 必填头：Upload-Length（正整数）
// 可选头：X-SHA256 / X-Compression（默认 none）/ X-Chunk-Size / X-File-Name
func parseTusInit(r *http.Request) (initSessionInput, error) {
	uploadLengthStr := r.Header.Get("Upload-Length")
	if uploadLengthStr == "" {
		return initSessionInput{}, domain.ErrInvalidArgument
	}
	uploadLength, err := strconv.ParseInt(uploadLengthStr, 10, 64)
	if err != nil || uploadLength < 0 {
		return initSessionInput{}, domain.ErrInvalidArgument
	}

	compression := r.Header.Get("X-Compression")
	if compression == "" {
		compression = string(domain.CompNone)
	}

	var chunkSize int64
	if chunkSizeStr := r.Header.Get("X-Chunk-Size"); chunkSizeStr != "" {
		chunkSize, _ = strconv.ParseInt(chunkSizeStr, 10, 64)
	}

	return initSessionInput{
		uploadLength: uploadLength,
		sha256:       r.Header.Get("X-SHA256"),
		compression:  domain.CompressionFormat(compression),
		chunkSize:    chunkSize,
		fileName:     decodeFileName(r.Header.Get("X-File-Name")),
	}, nil
}

// parseRestInit 解析 REST 协议的上传初始化请求。
//
// 必填查询参数：size（正整数）
// 可选头：X-SHA256 / X-Compression（默认 none）/ X-File-Name
// REST 协议不提供 Chunk-Size（chunkSize=0 让 service 用 UploadConfig.DefaultChunkSize）
func parseRestInit(r *http.Request) (initSessionInput, error) {
	lengthStr := r.URL.Query().Get("size")
	if lengthStr == "" {
		return initSessionInput{}, domain.ErrInvalidArgument
	}
	uploadLength, err := strconv.ParseInt(lengthStr, 10, 64)
	if err != nil || uploadLength < 0 {
		return initSessionInput{}, domain.ErrInvalidArgument
	}

	compression := r.Header.Get("X-Compression")
	if compression == "" {
		compression = string(domain.CompNone)
	}

	return initSessionInput{
		uploadLength: uploadLength,
		sha256:       r.Header.Get("X-SHA256"),
		compression:  domain.CompressionFormat(compression),
		chunkSize:    0,
		fileName:     decodeFileName(r.Header.Get("X-File-Name")),
	}, nil
}
