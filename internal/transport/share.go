package transport

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/bilbilmyc/fileupload/internal/domain"
)

// ShareHandler 分享链接 HTTP 处理器
type ShareHandler struct {
	shareSvc    *domain.ShareService
	downloadSvc *domain.DownloadService
}

// NewShareHandler 创建分享处理器
func NewShareHandler(shareSvc *domain.ShareService, downloadSvc *domain.DownloadService) *ShareHandler {
	return &ShareHandler{shareSvc: shareSvc, downloadSvc: downloadSvc}
}

// CreateShare POST /v1/share
func (h *ShareHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	namespace := GetNamespace(r.Context())
	entry, err := h.shareSvc.CreateShare(r.Context(), req, namespace)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	respondJSON(w, http.StatusCreated, entry)
}

// ListShares GET /v1/shares?file_id=... — 查询当前空间的可管理分享链接。
func (h *ShareHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	namespace := GetNamespace(r.Context())
	entries, err := h.shareSvc.ListShares(r.Context(), namespace, r.URL.Query().Get("file_id"))
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"shares": entries})
}

// RevokeShare DELETE /v1/shares/{token} — 立即撤销分享链接。
func (h *ShareHandler) RevokeShare(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}
	if err := h.shareSvc.RevokeShare(r.Context(), token, GetNamespace(r.Context())); err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AccessShare GET /s/{token}
func (h *ShareHandler) AccessShare(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/s/")
	if token == "" {
		respondError(w, http.StatusBadRequest, domain.ErrInvalidArgument)
		return
	}

	password := r.Header.Get("X-Share-Password")

	entry, err := h.shareSvc.AccessShare(r.Context(), token, password)
	if err != nil {
		if err == domain.ErrForbidden {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码错误", "code": "share_password_required"})
			return
		}
		respondError(w, domainErrorToStatus(err), err)
		return
	}

	// Tests can instantiate the handler without a download service to exercise
	// share validation only. Production always streams the file directly so public
	// links do not need access to an authenticated /v1/files endpoint.
	if h.downloadSvc == nil {
		http.Redirect(w, r, "/v1/files/"+entry.FileID+"?namespace="+entry.Namespace, http.StatusFound)
		return
	}

	rng, err := parseRangeHeader(r.Header.Get("Range"))
	if err != nil {
		w.Header().Set("Content-Range", "bytes */*")
		respondError(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}
	fileReader, err := h.downloadSvc.GetFile(r.Context(), entry.FileID, entry.Namespace, rng)
	if err != nil {
		respondError(w, domainErrorToStatus(err), err)
		return
	}
	defer fileReader.Reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-SHA256", fileReader.Blob.SHA256)
	w.Header().Set("Content-Disposition", contentDisposition("attachment", fileReader.File.Name))
	w.Header().Set("Cache-Control", "private, no-store")
	if fileReader.Range.Requested {
		w.Header().Set("Content-Range", formatContentRange(fileReader.Range.Offset, fileReader.Range.Length, fileReader.FileSize))
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(fileReader.Range.Length, 10))
		w.WriteHeader(http.StatusOK)
	}
	_, _ = io.Copy(w, fileReader.Reader)
}
