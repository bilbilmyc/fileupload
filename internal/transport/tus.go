package transport

import (
	"net/http"
	"strconv"

	"github.com/bilbilmyc/fileupload/internal/domain"
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
	in, err := parseTusInit(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	namespace := GetNamespace(r.Context())

	session, err := h.uploadSvc.CreateSession(r.Context(),
		in.sha256, in.uploadLength, in.compression,
		in.chunkSize, namespace, in.fileName)
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
	sessionID := r.PathValue("id")
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
	sessionID := r.PathValue("id")
	if sessionID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	offsetStr := r.Header.Get("Upload-Offset")
	offset, _ := strconv.ParseInt(offsetStr, 10, 64)
	_ = offset

	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	sliceSha256 := r.Header.Get("X-Slice-SHA256")
	sliceIndexStr := r.Header.Get("X-Slice-Index")
	sliceIndex := 0
	if sliceIndexStr != "" {
		sliceIndex, _ = strconv.Atoi(sliceIndexStr)
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	err := h.uploadSvc.AppendChunk(r.Context(), sessionID, sliceIndex, r.Body, sliceSha256)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	newOffset, _ := h.uploadSvc.GetOffset(r.Context(), sessionID)
	w.Header().Set("Upload-Offset", strconv.FormatInt(newOffset, 10))
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /uploads/{id} — 取消上传
func (h *TusHandler) CancelUpload(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
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

