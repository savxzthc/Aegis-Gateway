package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/savxzthc/aegis-gateway/internal/auth"
	"github.com/savxzthc/aegis-gateway/internal/backends"
)

var allowedUploadTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "application/pdf": true,
}

func (s *Server) UploadFile(w http.ResponseWriter, r *http.Request) {
	if s.Uploads == nil {
		writeError(w, http.StatusServiceUnavailable, "upload store unavailable", "UPLOADS_UNAVAILABLE")
		return
	}
	maxBytes := s.Config.Get().Server.MaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds configured size limit", "UPLOAD_TOO_LARGE")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "multipart field file is required", "UPLOAD_REQUIRED")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "upload read failed", "UPLOAD_READ_FAILED", err)
		return
	}
	if int64(len(data)) > maxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds configured size limit", "UPLOAD_TOO_LARGE")
		return
	}
	contentType := http.DetectContentType(data)
	if !allowedUploadTypes[contentType] {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported upload content type", "UPLOAD_TYPE_UNSUPPORTED")
		return
	}
	id, err := auth.GenerateID("upl")
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "upload id generation failed", "UPLOAD_SAVE_FAILED", err)
		return
	}
	if err := s.Uploads.SaveOwned(r.Context(), id, contentType, requestOwnerID(r), data); err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "upload save failed", "UPLOAD_SAVE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "content_type": contentType, "size": len(data), "name": header.Filename, "url": "/v1/uploads/" + id})
}

func (s *Server) GetUpload(w http.ResponseWriter, r *http.Request) {
	if s.Uploads == nil {
		writeError(w, http.StatusServiceUnavailable, "upload store unavailable", "UPLOADS_UNAVAILABLE")
		return
	}
	data, contentType, err := s.Uploads.Load(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "upload not found", "UPLOAD_NOT_FOUND")
		return
	}
	if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "upload read failed", "UPLOAD_READ_FAILED", err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	_, _ = w.Write(data)
}

func (s *Server) DeleteUpload(w http.ResponseWriter, r *http.Request) {
	if s.Uploads == nil {
		writeError(w, http.StatusServiceUnavailable, "upload store unavailable", "UPLOADS_UNAVAILABLE")
		return
	}
	if err := s.Uploads.Delete(r.Context(), chi.URLParam(r, "id")); errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "upload not found", "UPLOAD_NOT_FOUND")
		return
	} else if err != nil {
		writePrivateError(w, r, http.StatusInternalServerError, "upload delete failed", "UPLOAD_DELETE_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolveUploadImages(r *http.Request, messages []backends.ChatMessage) ([]backends.ChatMessage, bool, error) {
	out := append([]backends.ChatMessage(nil), messages...)
	hasImages := false
	for i, message := range out {
		parts := message.Content.Parts()
		changed := false
		for j, part := range parts {
			if part.Type != "image_url" || part.ImageURL == nil {
				continue
			}
			hasImages = true
			const prefix = "/v1/uploads/"
			if !strings.HasPrefix(part.ImageURL.URL, prefix) {
				continue
			}
			if s.Uploads == nil {
				return nil, false, fmt.Errorf("upload store unavailable")
			}
			id := strings.TrimPrefix(part.ImageURL.URL, prefix)
			data, contentType, err := s.Uploads.Load(r.Context(), id)
			if err != nil {
				return nil, false, err
			}
			parts[j].ImageURL = &backends.ImageURL{URL: "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)}
			changed = true
		}
		if changed {
			out[i].Content = backends.NewMessageParts(parts)
		}
	}
	return out, hasImages, nil
}
