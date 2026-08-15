package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/events"
)

// GET /api/events — the notification stream that keeps the panel current.
//
// SSE and not WebSocket because this only goes one way: the server notifies, the
// browser sends nothing. SSE is ordinary HTTP, so it crosses Traefik with no extra
// configuration and the browser reconnects on its own if it drops.
//
// With no Redis configured it answers 503 rather than leaving the connection
// hanging for ever: that way the panel knows there are no notifications here and
// falls back to reloading.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if s.bus == nil {
		respondError(w, http.StatusServiceUnavailable, "sin avisos en vivo")
		return
	}

	flush, puede := w.(http.Flusher)
	if !puede {
		respondError(w, http.StatusInternalServerError, "esta conexión no permite avisos")
		return
	}

	canal, err := s.bus.Subscribe(r.Context())
	if err != nil {
		s.log.Error("no se pudo escuchar los avisos", "error", err)
		respondError(w, http.StatusServiceUnavailable, "sin avisos en vivo")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Without this, a buffering proxy holds the notifications and delivers them all
	// at once at the end, which is exactly the opposite of the point.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush.Flush()

	// A heartbeat now and then: with nothing travelling, some middlebox declares the
	// connection dead and cuts it. The `:` comment is an empty event the browser
	// ignores.
	latido := time.NewTicker(25 * time.Second)
	defer latido.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-latido.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flush.Flush()

		case e, abierto := <-canal:
			if !abierto {
				return
			}
			datos, err := json.Marshal(e)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("event: " + e.Type + "\ndata: " + string(datos) + "\n\n")); err != nil {
				return
			}
			flush.Flush()
		}
	}
}

// notify publishes without getting in the way: notifications are an extra, and if
// Redis does not answer, whatever was being done must not fall over.
func (s *Server) notify(r *http.Request, e events.Event) {
	if s.bus == nil {
		return
	}
	if err := s.bus.Publish(r.Context(), e); err != nil {
		s.log.Warn("no se pudo publicar el aviso", "tipo", e.Type, "error", err)
	}
}
