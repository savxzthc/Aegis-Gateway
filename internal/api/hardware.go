package api

import "net/http"

// Hardware handles GET /v1/hardware.
func (s *Server) Hardware(w http.ResponseWriter, r *http.Request) {
	info, err := s.HardwareProvider.Query(r.Context())
	if err != nil {
		writePrivateError(w, r, http.StatusServiceUnavailable, "hardware query failed", "HARDWARE_QUERY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}
