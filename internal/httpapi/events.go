package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"phantom-mail/internal/hub"
)

// events streams mailbox changes as Server-Sent Events:
//
//	event: message   data: <message summary JSON>
//	event: deleted   data: {"id": "..."}
//
// A comment line is sent periodically to keep proxies from closing the stream.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	box, ok := mailboxParam(w, r)
	if !ok {
		return
	}
	client := s.clientIP(r)
	if !s.acquireSSE(client) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many open event streams")
		return
	}
	defer s.releaseSSE(client)

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // streams outlive the server write timeout

	// Subscribe before sending headers so anything delivered after the client
	// sees the response is guaranteed to be streamed.
	ch, cancel := s.svc.Subscribe(box)
	defer cancel()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // ask nginx-style proxies not to buffer
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	if err := rc.Flush(); err != nil {
		return
	}

	tick := time.NewTicker(s.cfg.HeartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
		case ev, open := <-ch:
			if !open {
				return
			}
			if err := writeEvent(w, ev); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func writeEvent(w http.ResponseWriter, ev hub.Event) error {
	var data []byte
	var err error
	switch ev.Type {
	case hub.EventMessage:
		data, err = json.Marshal(ev.Summary)
	case hub.EventDeleted:
		data, err = json.Marshal(map[string]string{"id": ev.ID})
	default:
		return nil
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
	return err
}

func (s *Server) acquireSSE(client string) bool {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.sse[client] >= s.cfg.MaxSSEPerClient {
		return false
	}
	s.sse[client]++
	return true
}

func (s *Server) releaseSSE(client string) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	if s.sse[client]--; s.sse[client] <= 0 {
		delete(s.sse, client)
	}
}
