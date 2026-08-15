package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/events"
)

// GET /api/events — el flujo de avisos que mantiene el panel al día.
//
// Es SSE y no WebSocket porque esto va en un solo sentido: el servidor avisa, el
// navegador no manda nada. SSE es HTTP normal, así que atraviesa Traefik sin
// configuración aparte y el navegador reconecta solo si se corta.
//
// Si no hay Redis configurado se responde 503 en vez de dejar la conexión colgada
// para siempre: así el panel sabe que aquí no hay avisos y se queda con su
// recarga periódica.
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
	// Sin esto, un proxy con almacenamiento intermedio se guarda los avisos y los
	// entrega todos juntos al final, que es justo lo contrario de lo que se busca.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush.Flush()

	// Un latido de vez en cuando: si no viaja nada, algún intermediario da la
	// conexión por muerta y la corta. El comentario `:` es un evento vacío que el
	// navegador ignora.
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

// notify publica sin estorbar: los avisos son un extra, y si Redis no responde no
// puede caerse lo que se estaba haciendo.
func (s *Server) notify(r *http.Request, e events.Event) {
	if s.bus == nil {
		return
	}
	if err := s.bus.Publish(r.Context(), e); err != nil {
		s.log.Warn("no se pudo publicar el aviso", "tipo", e.Type, "error", err)
	}
}
