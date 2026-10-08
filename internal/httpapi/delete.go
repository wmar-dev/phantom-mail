package httpapi

import (
	"errors"
	"net/http"

	"phantom-mail/internal/message"
	"phantom-mail/internal/store"
)

func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !message.ValidID(id) {
		writeError(w, http.StatusNotFound, "not_found", "no such message")
		return
	}
	switch err := s.svc.Delete(box, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such message")
	case err != nil:
		s.internal(w, "delete message", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) emptyMailbox(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	if err := s.svc.DeleteMailbox(box); err != nil {
		s.internal(w, "empty mailbox", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
