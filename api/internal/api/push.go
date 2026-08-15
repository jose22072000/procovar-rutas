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

// POST /api/ingesta/fichero — la puerta para n8n.
//
// El vendedor sube su .gpx a Drive una vez al día; n8n vigila las carpetas y
// manda aquí cada fichero nuevo. Es el mismo patrón que ya usa PEDIDO con
// /orders/bulk, así que el flujo de n8n se parece al que ya existe.
//
// No lleva sesión de usuario sino clave de servicio: quien llama es una máquina.
// Y es idempotente por `driveFileId`, de modo que si n8n reintenta —o si el
// barrido nocturno pasa después por el mismo fichero— no se duplica nada.

type pushRequest struct {
	SourceID    string   `json:"sourceId"`
	FolderID    string   `json:"folderId"`
	DriveFileID string   `json:"driveFileId"`
	Name        string   `json:"name"`
	FolderPath  []string `json:"folderPath"`
	Created     string   `json:"createdAt"`
	// ContentBase64 es lo que manda n8n en `{{ $binary.data.data }}`.
	ContentBase64 string `json:"contentBase64"`
}

func (s *Server) receiveFile(w http.ResponseWriter, r *http.Request) {
	if !s.validServiceKey(r) {
		respondError(w, http.StatusUnauthorized, "clave de servicio inválida")
		return
	}

	var p pushRequest
	// 32 MiB de tope: un .gpx de una jornada no llega a 1 MiB, y sin límite una
	// petición mal formada se come la memoria del servidor.
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

	// Por defecto se ENCOLA y se contesta enseguida: n8n no tiene por qué esperar
	// a que se parseen 2 500 puntos, y una subida de toda la plantilla a la misma
	// hora no puede tumbar el panel. Con ?sync=1 se procesa en el acto, igual que
	// en la ingesta de PEDIDO, que es lo cómodo para depurar.
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
			// 503 y no 400: el fichero está bien, el que falla somos nosotros.
			// Con 503 n8n sabe que tiene que reintentar.
			respondError(w, http.StatusServiceUnavailable, "la cola no está disponible")
			return
		}
		// El panel de administración enseña la queue: que se entere al momento.
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
		// 400 y no 500: casi siempre es una carpeta sin dar de alta o un campo
		// que falta, y n8n tiene que poder distinguir "corrígelo" de "reintenta".
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

// validServiceKey compara en tiempo constante, para no filtrar la clave
// carácter a carácter con la duración de la respuesta.
func (s *Server) validServiceKey(r *http.Request) bool {
	esperada := s.cfg.ClaveServicio
	if esperada == "" {
		return false // sin clave configurada, la puerta está cerrada
	}
	recibida := r.Header.Get("x-api-key")
	return subtle.ConstantTimeCompare([]byte(recibida), []byte(esperada)) == 1
}

// queueStats es lo que enseña la pantalla de administración: cuántos ficheros
// esperan, cuántos se están procesando y cuántos se apartaron.
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
