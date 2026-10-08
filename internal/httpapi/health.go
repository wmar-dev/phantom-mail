package httpapi

import "net/http"

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	st := s.svc.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"uptime_seconds": int(s.cfg.Clock.Now().Sub(s.started).Seconds()),
		"messages":       st.Messages,
		"store_bytes":    st.Bytes,
	})
}
