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
}

// NewDownloadHandler 创建下载处理器
func NewDownloadHandler(downloadSvc *domain.DownloadService) *DownloadHandler {
	return &DownloadHandler{downloadSvc: downloadSvc}
}

// GetFile GET /v1/files/{id} — 单文件下载
func (h *DownloadHandler) GetFile(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	rng := parseRangeHeader(r.Header.Get("Range"))

	fileReader, err := h.downloadSvc.GetFile(r.Context(), fileID, namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()

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

// GetPreview GET /v1/preview/{id} — 文件预览（流式、内联、正确 Content-Type）
func (h *DownloadHandler) GetPreview(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	namespace := GetNamespace(r.Context())

	rng := parseRangeHeader(r.Header.Get("Range"))

	fileReader, err := h.downloadSvc.GetFile(r.Context(), fileID, namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()

	mimeType := guessMimeType(fileReader.File.Name)

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-SHA256", fileReader.Blob.SHA256)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Disposition", "inline; filename=\""+fileReader.File.Name+"\"")

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
