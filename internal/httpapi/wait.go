package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"phantom-mail/internal/message"
)

// waitForMessage is a long poll: it answers as soon as the mailbox holds a
// message newer than ?after (any message when omitted) and otherwise holds the
// request open for ?timeout seconds, then answers 204.
func (s *Server) waitForMessage(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	timeout := 30
	if v := r.URL.Query().Get("timeout"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 60 {
			writeError(w, http.StatusBadRequest, "bad_request", "timeout must be a whole number of seconds from 1 to 60")
			return
		}
		timeout = n
	}
	after := r.URL.Query().Get("after")
	if after != "" && !message.ValidID(after) {
		writeError(w, http.StatusBadRequest, "bad_request", "after must be a message id")
		return
	}
	d := time.Duration(timeout) * time.Second
	if d > s.cfg.MaxWait {
		d = s.cfg.MaxWait
	}

	// Also stop when the server is closing so shutdown is not held up.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-s.closing:
			cancel()
		case <-ctx.Done():
		}
	}()

	list, err := s.svc.Wait(ctx, box, after, d)
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		w.WriteHeader(http.StatusNoContent)
	case err != nil:
		s.internal(w, "wait", err)
	case len(list) == 0:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"messages": summariesOrEmpty(list)})
	}
}
