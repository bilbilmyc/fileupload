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

	setTusHeaders(w)
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

	namespace := GetNamespace(r.Context())
	offset, err := h.uploadSvc.GetOffsetForNamespace(r.Context(), sessionID, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	setTusHeaders(w)
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
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if offsetStr == "" || err != nil || offset < 0 {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	namespace := GetNamespace(r.Context())
	newOffset, err := h.uploadSvc.AppendChunkAtOffsetForNamespace(
		r.Context(), sessionID, namespace, offset, r.Body, r.Header.Get("X-Slice-SHA256"),
	)
	if err != nil {
		if err == domain.ErrOffsetConflict {
			if current, currentErr := h.uploadSvc.GetOffsetForNamespace(r.Context(), sessionID, namespace); currentErr == nil {
				setTusHeaders(w)
				w.Header().Set("Upload-Offset", strconv.FormatInt(current, 10))
			}
		}
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	setTusHeaders(w)
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

	namespace := GetNamespace(r.Context())
	if err := h.uploadSvc.AbortForNamespace(r.Context(), sessionID, namespace); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setTusHeaders emits the mandatory resumable-upload protocol negotiation headers.
func setTusHeaders(w http.ResponseWriter) {
	w.Header().Set("Tus-Resumable", "1.0.0")
	w.Header().Set("Tus-Version", "1.0.0")
	w.Header().Set("Tus-Extension", "creation,termination")
}
