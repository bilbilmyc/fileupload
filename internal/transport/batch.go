package transport

import (
	"io"
	"net/http"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// BatchHandler 批量操作 HTTP 处理器
type BatchHandler struct {
	batchSvc *domain.BatchService
}

// NewBatchHandler 创建批量操作处理器
func NewBatchHandler(batchSvc *domain.BatchService) *BatchHandler {
	return &BatchHandler{batchSvc: batchSvc}
}

// batchRequest 批量操作通用请求体
type batchRequest struct {
	IDs         []string `json:"ids"`
	TargetDirID string   `json:"target_dir_id,omitempty"`
	Format      string   `json:"format,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// POST /v1/batch/delete
func (h *BatchHandler) BatchDelete(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if len(req.IDs) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	result, err := h.batchSvc.BatchDelete(r.Context(), req.IDs, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// GET /v1/batch/download — 流式批量下载，ids 用逗号分隔
func (h *BatchHandler) BatchDownloadGet(w http.ResponseWriter, r *http.Request) {
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	req := batchRequest{
		IDs:    splitCSV(idsParam),
		Format: r.URL.Query().Get("format"),
	}
	if len(req.IDs) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	format := domain.CompZip
	if req.Format != "" {
		format = domain.CompressionFormat(req.Format)
	}

	namespace := GetNamespace(r.Context())
	reader, err := h.batchSvc.BatchDownload(r.Context(), req.IDs, namespace, format)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer reader.Close()

	filename := "batch." + string(format)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	switch format {
	case domain.CompZip:
		w.Header().Set("Content-Type", "application/zip")
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

// POST /v1/batch/download
func (h *BatchHandler) BatchDownload(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if len(req.IDs) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	// 默认使用 zip 格式
	format := domain.CompZip
	if req.Format != "" {
		format = domain.CompressionFormat(req.Format)
	}

	namespace := GetNamespace(r.Context())
	reader, err := h.batchSvc.BatchDownload(r.Context(), req.IDs, namespace, format)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer reader.Close()

	// 设置响应头
	filename := "batch." + string(format)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	switch format {
	case domain.CompZip:
		w.Header().Set("Content-Type", "application/zip")
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

// POST /v1/batch/move
func (h *BatchHandler) BatchMove(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if len(req.IDs) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	if err := h.batchSvc.BatchMove(r.Context(), req.IDs, req.TargetDirID, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /v1/batch/copy
func (h *BatchHandler) BatchCopy(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if len(req.IDs) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	if err := h.batchSvc.BatchCopy(r.Context(), req.IDs, req.TargetDirID, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /v1/batch/tags
func (h *BatchHandler) BatchTags(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if len(req.IDs) == 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	if err := h.batchSvc.BatchTag(r.Context(), req.IDs, req.Tags, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// splitCSV 将逗号分隔的字符串拆分为切片
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
