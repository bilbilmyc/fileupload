package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mayc/casdao/fileupload/internal/domain"
)

// TusHandler tus.io 协议处理器
type TusHandler struct {
	uploadSvc *domain.UploadService
}

// NewTusHandler 创建 tus 处理器
func NewTusHandler(uploadSvc *domain.UploadService) *TusHandler {
	return &TusHandler{uploadSvc: uploadSvc}
}

// POST /uploads — 创建上传会话
func (h *TusHandler) CreateUpload(w http.ResponseWriter, r *http.Request) {
	// 读取 tus 头
	uploadLengthStr := r.Header.Get("Upload-Length")
	if uploadLengthStr == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	uploadLength, err := strconv.ParseInt(uploadLengthStr, 10, 64)
	if err != nil || uploadLength < 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	sha256 := r.Header.Get("X-SHA256")
	compression := r.Header.Get("X-Compression")
	if compression == "" {
		compression = "none"
	}
	chunkSizeStr := r.Header.Get("X-Chunk-Size")
	var chunkSize int64
	if chunkSizeStr != "" {
		chunkSize, _ = strconv.ParseInt(chunkSizeStr, 10, 64)
	}
	fileName := decodeFileName(r.Header.Get("X-File-Name"))
	namespace := GetNamespace(r.Context())

	session, err := h.uploadSvc.CreateSession(r.Context(),
		sha256, uploadLength, domain.CompressionFormat(compression),
		chunkSize, namespace, fileName)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	w.Header().Set("Location", "/uploads/"+session.SessionID)
	w.Header().Set("Upload-Offset", "0")
	w.WriteHeader(http.StatusCreated)
}

// HEAD /uploads/{id} — 获取上传偏移量
func (h *TusHandler) GetUploadInfo(w http.ResponseWriter, r *http.Request) {
	sessionID := extractPathID(r.URL.Path, "/uploads/")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	offset, err := h.uploadSvc.GetOffset(r.Context(), sessionID)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	_, _, _ = h.uploadSvc.GetStatus(r.Context(), sessionID)

	w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
	w.WriteHeader(http.StatusOK)
}

// PATCH /uploads/{id} — 追加分片
func (h *TusHandler) AppendChunk(w http.ResponseWriter, r *http.Request) {
	sessionID := extractPathID(r.URL.Path, "/uploads/")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	// 从 X-Slice-Index 或 Upload-Offset 推算分片 index
	offsetStr := r.Header.Get("Upload-Offset")
	offset, _ := strconv.ParseInt(offsetStr, 10, 64)
	_ = offset // 可用于校验

	sliceSha256 := r.Header.Get("X-Slice-SHA256")
	sliceIndexStr := r.Header.Get("X-Slice-Index")
	sliceIndex := 0
	if sliceIndexStr != "" {
		sliceIndex, _ = strconv.Atoi(sliceIndexStr)
	}

	// 限制 body 大小（单个分片不超过 50MB）
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	err := h.uploadSvc.AppendChunk(r.Context(), sessionID, sliceIndex, r.Body, sliceSha256)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	// 更新后的 offset
	newOffset, _ := h.uploadSvc.GetOffset(r.Context(), sessionID)
	w.Header().Set("Upload-Offset", strconv.FormatInt(newOffset, 10))
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /uploads/{id} — 取消上传
func (h *TusHandler) CancelUpload(w http.ResponseWriter, r *http.Request) {
	sessionID := extractPathID(r.URL.Path, "/uploads/")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	if err := h.uploadSvc.Abort(r.Context(), sessionID); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- REST Handler ----------

// RESTHandler 自定义 REST 分片上传处理器
type RESTHandler struct {
	uploadSvc   *domain.UploadService
	downloadSvc *domain.DownloadService
}

// NewRESTHandler 创建 REST 处理器
func NewRESTHandler(uploadSvc *domain.UploadService, downloadSvc *domain.DownloadService) *RESTHandler {
	return &RESTHandler{uploadSvc: uploadSvc, downloadSvc: downloadSvc}
}

// InitUpload POST /v1/uploads/init
func (h *RESTHandler) InitUpload(w http.ResponseWriter, r *http.Request) {
	// 同 tus 创建逻辑，共享 UploadService
	lengthStr := r.URL.Query().Get("size")
	if lengthStr == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	uploadLength, _ := strconv.ParseInt(lengthStr, 10, 64)
	if uploadLength < 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	sha256 := r.Header.Get("X-SHA256")
	compression := r.Header.Get("X-Compression")
	fileName := decodeFileName(r.Header.Get("X-File-Name"))
	if compression == "" {
		compression = "none"
	}
	namespace := GetNamespace(r.Context())
	
	session, err := h.uploadSvc.CreateSession(r.Context(), sha256, uploadLength,
		domain.CompressionFormat(compression), 0, namespace, fileName)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"session_id": session.SessionID,
		"chunk_size": session.ChunkSize,
	})
}

// UploadChunk PUT /v1/uploads/{id}/chunks/{index}
func (h *RESTHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	// URL: /v1/uploads/{id}/chunks/{index}
	parts := strings.Split(r.URL.Path, "/")
	// ["", "v1", "uploads", sessionID, "chunks", index]
	if len(parts) < 6 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	sessionID := parts[3]
	index, _ := strconv.Atoi(parts[5])
	sliceSha256 := r.Header.Get("X-Slice-SHA256")

	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	err := h.uploadSvc.AppendChunk(r.Context(), sessionID, index, r.Body, sliceSha256)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetUploadStatus GET /v1/uploads/{id}/status
func (h *RESTHandler) GetUploadStatus(w http.ResponseWriter, r *http.Request) {
	sessionID := extractPathID(r.URL.Path, "/v1/uploads/")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	chunks, total, err := h.uploadSvc.GetStatus(r.Context(), sessionID)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"chunks":     chunks,
		"total":      total,
	})
}

// FinalizeUpload POST /v1/uploads/{id}/finalize
func (h *RESTHandler) FinalizeUpload(w http.ResponseWriter, r *http.Request) {
	sessionID := extractPathID(r.URL.Path, "/v1/uploads/")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	result, err := h.uploadSvc.Finalize(r.Context(), sessionID)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"file_id": result.FileID,
		"sha256":  result.SHA256,
		"size":    result.Size,
		"name":    result.Name,
	})
}

// SubmitDir POST /v1/dirs
func (h *RESTHandler) SubmitDir(w http.ResponseWriter, r *http.Request) {
	var manifest domain.DirManifest
	if err := decodeJSON(r.Body, &manifest); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	dir, err := h.uploadSvc.SubmitDir(r.Context(), manifest, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]string{
		"file_id": dir.FileID,
	})
}

// ---------- Download Handler ----------

// DownloadHandler 下载处理器
type DownloadHandler struct {
	downloadSvc *domain.DownloadService
}

// NewDownloadHandler 创建下载处理器
func NewDownloadHandler(downloadSvc *domain.DownloadService) *DownloadHandler {
	return &DownloadHandler{downloadSvc: downloadSvc}
}

// GetFile GET /v1/files/{id} — 单文件下载
func (h *DownloadHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	fileID := extractPathID(r.URL.Path, "/v1/files/")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	// 解析 Range 头
	rng := parseRangeHeader(r.Header.Get("Range"))

	fileReader, err := h.downloadSvc.GetFile(r.Context(), fileID, namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()

	// 设置响应头
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-SHA256", fileReader.Blob.SHA256)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fileReader.File.Name+"\"")

	if rng.IsZero() {
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Blob.Size, 10))
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Content-Range", formatContentRange(rng.Offset, rng.Length, fileReader.FileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(rng.Length, 10))
		w.WriteHeader(http.StatusPartialContent)
	}

	io.Copy(w, fileReader.Reader)
}

// GetDir GET /v1/dirs/{id} — 目录流式打包下载
func (h *DownloadHandler) GetDir(w http.ResponseWriter, r *http.Request) {
	dirID := extractPathID(r.URL.Path, "/v1/dirs/")
	if dirID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())
	formatStr := r.URL.Query().Get("format")
	if formatStr == "" {
		formatStr = "tar.gz"
	}
	format := domain.CompressionFormat(formatStr)

	// 先获取 manifest（含 X-Tree-SHA256）
	dw, err := h.downloadSvc.GetDirManifest(r.Context(), dirID, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	// 流式打包
	reader, err := h.downloadSvc.StreamDir(r.Context(), dw, format)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	defer reader.Close()

	// 设置响应头
	w.Header().Set("X-Tree-SHA256", dw.TreeSHA256)
	w.Header().Set("Content-Disposition", "attachment; filename=\"dir."+formatStr+"\"")

	switch format {
	case domain.CompTarGz:
		w.Header().Set("Content-Type", "application/gzip")
	case domain.CompTarZst:
		w.Header().Set("Content-Type", "application/zstd")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

// ---------- 管理 Handler ----------

// DeleteFile DELETE /v1/files/{id}
func (h *RESTHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	fileID := extractPathID(r.URL.Path, "/v1/files/")
	namespace := GetNamespace(r.Context())

	if err := h.uploadSvc.Delete(r.Context(), fileID, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListDir GET /v1/ls
func (h *RESTHandler) ListDir(w http.ResponseWriter, r *http.Request) {
	parentID := r.URL.Query().Get("parent")
	namespace := GetNamespace(r.Context())

	dir, children, err := h.downloadSvc.ListDir(r.Context(), parentID, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"dir":      dir,
		"children": children,
	})
}

// StatFile GET /v1/stat/{id}
func (h *RESTHandler) StatFile(w http.ResponseWriter, r *http.Request) {
	id := extractPathID(r.URL.Path, "/v1/stat/")
	namespace := GetNamespace(r.Context())

	file, blob, err := h.downloadSvc.Stat(r.Context(), id, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"file": file,
		"blob": blob,
	})
}

// ======== 工具函数 ========

// extractPathID 从 URL 路径中提取 ID
// 如 /uploads/abc123 → abc123
// 如 /v1/uploads/abc123/status → abc123
func extractPathID(path, prefix string) string {
	if len(path) <= len(prefix) {
		return ""
	}
	id := strings.TrimPrefix(path, prefix)
	// 去掉尾部斜杠和后续路径段
	if idx := strings.IndexByte(id, '/'); idx >= 0 {
		id = id[:idx]
	}
	return id
}

// parseRangeHeader 解析 HTTP Range 头
// 支持格式: bytes=0-1023
func parseRangeHeader(rangeStr string) domain.DownloadRange {
	if !strings.HasPrefix(rangeStr, "bytes=") {
		return domain.DownloadRange{}
	}
	rangeStr = strings.TrimPrefix(rangeStr, "bytes=")
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		return domain.DownloadRange{}
	}
	start, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	end, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if end > 0 && end >= start {
		return domain.DownloadRange{Offset: start, Length: end - start + 1}
	}
	return domain.DownloadRange{Offset: start}
}

// formatContentRange 格式化 Content-Range 响应头
func formatContentRange(offset, length, total int64) string {
	return "bytes " + strconv.FormatInt(offset, 10) + "-" +
		strconv.FormatInt(offset+length-1, 10) + "/" +
		strconv.FormatInt(total, 10)
}

// decodeJSON 解码 JSON 请求体
func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// decodeFileName 对 X-File-Name 头值进行智能解码。
// 某些客户端（如浏览器）可能对 non-ASCII 文件名做了 URL 编码，兼容处理。
func decodeFileName(raw string) string {
	if raw == "" {
		return ""
	}
	// 如果包含 % 且解码后不是纯 ASCII，说明是 URL 编码的
	if strings.Contains(raw, "%") {
		if decoded, err := url.QueryUnescape(raw); err == nil && decoded != raw {
			return decoded
		}
	}
	return raw
}
