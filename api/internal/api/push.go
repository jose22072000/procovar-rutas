package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"github.com/procovar/procovar-rutas/api/internal/events"
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/queue"
)

// POST /api/ingest/file — the door for n8n.
//
// The seller uploads their .gpx to Drive once a day; n8n watches the folders and
// sends every new file here. It is the same pattern PEDIDO already uses with
// /orders/bulk, so the n8n flow looks like the one that already exists.
//
// It carries a service key rather than a user session: the caller is a machine.
// And it is idempotent on `driveFileId`, so if n8n retries — or if the nightly
// scan later passes over the same file — nothing is duplicated.

type pushRequest struct {
	SourceID    string   `json:"sourceId"`
	FolderID    string   `json:"folderId"`
	DriveFileID string   `json:"driveFileId"`
	Name        string   `json:"name"`
	FolderPath  []string `json:"folderPath"`
	Created     string   `json:"createdAt"`
	// ContentBase64 is what n8n sends in `{{ $binary.data.data }}`.
	ContentBase64 string `json:"contentBase64"`
}

func (s *Server) receiveFile(w http.ResponseWriter, r *http.Request) {
	if !s.validServiceKey(r) {
		respondError(w, http.StatusUnauthorized, "clave de servicio inválida")
		return
	}

	var p pushRequest
	// A 32 MiB cap: a workday's .gpx does not reach 1 MiB, and with no limit a
	// malformed request would eat the server's memory.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, "cuerpo inválido")
		return
	}

	contenido, err := base64.StdEncoding.DecodeString(p.ContentBase64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "el contenido no es base64 válido")
		return
	}
	if p.DriveFileID == "" || p.Name == "" || len(contenido) == 0 {
		respondError(w, http.StatusBadRequest, "faltan driveFileId, nombre o contenido")
		return
	}

	creado := time.Now().UTC()
	if p.Created != "" {
		if t, err := time.Parse(time.RFC3339, p.Created); err == nil {
			creado = t.UTC()
		}
	}

	// By default it ENQUEUES and answers straight away: n8n has no business waiting
	// for 2,500 points to be parsed, and the whole workforce uploading at the same
	// hour cannot take the panel down. With ?sync=1 it is processed on the spot, as
	// in PEDIDO's ingest, which is the convenient thing when debugging.
	if s.queue != nil && r.URL.Query().Get("sync") != "1" {
		err := s.queue.Enqueue(r.Context(), queue.Job{
			SourceID:      p.SourceID,
			FolderID:      p.FolderID,
			DriveFileID:   p.DriveFileID,
			Name:          p.Name,
			FolderPath:    p.FolderPath,
			Created:       creado,
			ContentBase64: p.ContentBase64,
		})
		if err != nil {
			s.log.Error("no se pudo encolar", "fichero", p.Name, "error", err)
			// 503 and not 400: the file is fine, we are the ones failing. With a 503
			// n8n knows it has to retry.
			respondError(w, http.StatusServiceUnavailable, "la cola no está disponible")
			return
		}
		// The administration panel shows the queue: let it know right away.
		s.notify(r, events.Event{Type: events.TypeQueue, Detail: "encolado"})
		respond(w, http.StatusAccepted, map[string]any{"ok": true, "queued": true})
		return
	}

	nuevo, puntos, err := s.ingest.Receive(r.Context(), ingest.Pushed{
		SourceID:    p.SourceID,
		FolderID:    p.FolderID,
		DriveFileID: p.DriveFileID,
		Name:        p.Name,
		FolderPath:  p.FolderPath,
		Created:     creado,
		Content:     contenido,
	})
	if err != nil {
		// 400 and not 500: it is almost always an unregistered folder or a missing
		// field, and n8n has to tell "fix it" apart from "retry".
		s.log.Warn("empuje rechazado", "fichero", p.Name, "error", err)
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respond(w, http.StatusOK, map[string]any{
		"ok":     true,
		"added":  nuevo,
		"points": puntos,
	})
}

// validServiceKey compares in constant time, so the key is not leaked character
// by character through the response time.
func (s *Server) validServiceKey(r *http.Request) bool {
	esperada := s.cfg.ServiceKey
	if esperada == "" {
		return false // sin clave configurada, la puerta está cerrada
	}
	recibida := r.Header.Get("x-api-key")
	return subtle.ConstantTimeCompare([]byte(recibida), []byte(esperada)) == 1
}

// queueStats is what the administration screen shows: how many files are waiting,
// how many are being processed and how many were set aside.
func (s *Server) queueStats(w http.ResponseWriter, r *http.Request) {
	if s.queue == nil {
		respond(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	e, err := s.queue.Stats(r.Context())
	if err != nil {
		s.fail(w, "estado de la cola", err)
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"active":     true,
		"pending":    e.Pending,
		"processing": e.Processing,
		"failed":     e.Failed,
	})
}
