package transport

import (
	"io"
	"net/http"
	"strconv"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// DownloadHandler 下载处理器
type DownloadHandler struct {
	downloadSvc *domain.DownloadService
	audit       auditLogger
}

// NewDownloadHandler 创建下载处理器
func NewDownloadHandler(downloadSvc *domain.DownloadService) *DownloadHandler {
	return &DownloadHandler{downloadSvc: downloadSvc}
}

// WithAuditLogger 为下载、预览操作补充非阻塞审计记录。
func (h *DownloadHandler) WithAuditLogger(logger auditLogger) *DownloadHandler {
	h.audit = logger
	return h
}

// GetFile GET /v1/files/{id} — 单文件下载
func (h *DownloadHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	rng, err := parseRangeHeader(r.Header.Get("Range"))
	if err != nil {
		w.Header().Set("Content-Range", "bytes */*")
		respondError(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}

	fileReader, err := h.downloadSvc.GetFile(r.Context(), fileID, namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()
	writeAudit(h.audit, r.Context(), "download", "file", fileReader.File.FileID, namespace, "single file download")

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-SHA256", fileReader.Blob.SHA256)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", fileReader.File.Name))
	w.Header().Set("Cache-Control", "private, no-store")

	if !fileReader.Range.Requested {
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Content-Range", formatContentRange(fileReader.Range.Offset, fileReader.Range.Length, fileReader.FileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusPartialContent)
	}

	io.Copy(w, fileReader.Reader)
}

// GetPreview GET /v1/preview/{id} — 文件预览（流式、内联、正确 Content-Type）
func (h *DownloadHandler) GetPreview(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	rng, err := parseRangeHeader(r.Header.Get("Range"))
	if err != nil {
		w.Header().Set("Content-Range", "bytes */*")
		respondError(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}

	fileReader, err := h.downloadSvc.GetFile(r.Context(), fileID, namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()
	writeAudit(h.audit, r.Context(), "preview", "file", fileReader.File.FileID, namespace, "inline file preview")

	mimeType := guessMimeType(fileReader.File.Name)

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-SHA256", fileReader.Blob.SHA256)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", contentDisposition("inline", fileReader.File.Name))

	if !fileReader.Range.Requested {
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusOK)
	} else {
		w.Header().Set("Content-Range", formatContentRange(fileReader.Range.Offset, fileReader.Range.Length, fileReader.FileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusPartialContent)
	}

	io.Copy(w, fileReader.Reader)
}

// GetDir GET /v1/dirs/{id} — 目录流式打包下载
func (h *DownloadHandler) GetDir(w http.ResponseWriter, r *http.Request) {
	dirID := r.PathValue("id")
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
	if format != domain.CompTarGz && format != domain.CompTarZst && format != domain.CompZip {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	dw, err := h.downloadSvc.GetDirManifest(r.Context(), dirID, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	reader, err := h.downloadSvc.StreamDir(r.Context(), dw, format)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	defer reader.Close()
	writeAudit(h.audit, r.Context(), "download", "directory", dirID, namespace, "directory archive download")

	w.Header().Set("X-Tree-SHA256", dw.TreeSHA256)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", "dir."+formatStr))
	w.Header().Set("Cache-Control", "private, no-store")

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
