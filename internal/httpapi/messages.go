package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"phantom-mail/internal/message"
	"phantom-mail/internal/store"
)

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a whole number from 1 to 100")
			return
		}
		limit = n
	}
	after := r.URL.Query().Get("after")
	if after != "" && !message.ValidID(after) {
		writeError(w, http.StatusBadRequest, "bad_request", "after must be a message id")
		return
	}
	list, err := s.svc.List(box, after, limit)
	if err != nil {
		s.internal(w, "list messages", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": summariesOrEmpty(list)})
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	sum, raw, found := s.fetch(w, box, r.PathValue("id"))
	if !found {
		return
	}
	writeJSON(w, http.StatusOK, message.Full(sum, message.Parse(raw)))
}

// fetch loads a message, writing a 404 (or 500) response when unavailable.
func (s *Server) fetch(w http.ResponseWriter, box, id string) (message.Summary, []byte, bool) {
	if !message.ValidID(id) {
		writeError(w, http.StatusNotFound, "not_found", "no such message")
		return message.Summary{}, nil, false
	}
	sum, raw, err := s.svc.Get(box, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such message")
		return message.Summary{}, nil, false
	case err != nil:
		s.internal(w, "get message", err)
		return message.Summary{}, nil, false
	}
	return sum, raw, true
}

func (s *Server) internal(w http.ResponseWriter, what string, err error) {
	s.cfg.Logger.Error(what+" failed", "error", err.Error())
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}
