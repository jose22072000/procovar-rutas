package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/cola"
	"github.com/procovar/procovar-rutas/api/internal/ingesta"
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

type peticionEmpuje struct {
	FuenteID    string   `json:"sourceId"`
	FolderID    string   `json:"folderId"`
	DriveFileID string   `json:"driveFileId"`
	Nombre      string   `json:"name"`
	RutaCarpeta []string `json:"folderPath"`
	Creado      string   `json:"createdAt"`
	// ContenidoBase64 es lo que manda n8n en `{{ $binary.data.data }}`.
	ContenidoBase64 string `json:"contenidoBase64"`
}

func (s *Servidor) recibirFichero(w http.ResponseWriter, r *http.Request) {
	if !s.claveDeServicioValida(r) {
		responderError(w, http.StatusUnauthorized, "clave de servicio inválida")
		return
	}

	var p peticionEmpuje
	// 32 MiB de tope: un .gpx de una jornada no llega a 1 MiB, y sin límite una
	// petición mal formada se come la memoria del servidor.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&p); err != nil {
		responderError(w, http.StatusBadRequest, "cuerpo inválido")
		return
	}

	contenido, err := base64.StdEncoding.DecodeString(p.ContenidoBase64)
	if err != nil {
		responderError(w, http.StatusBadRequest, "el contenido no es base64 válido")
		return
	}
	if p.DriveFileID == "" || p.Nombre == "" || len(contenido) == 0 {
		responderError(w, http.StatusBadRequest, "faltan driveFileId, nombre o contenido")
		return
	}

	creado := time.Now().UTC()
	if p.Creado != "" {
		if t, err := time.Parse(time.RFC3339, p.Creado); err == nil {
			creado = t.UTC()
		}
	}

	// Por defecto se ENCOLA y se contesta enseguida: n8n no tiene por qué esperar
	// a que se parseen 2 500 puntos, y una subida de toda la plantilla a la misma
	// hora no puede tumbar el panel. Con ?sync=1 se procesa en el acto, igual que
	// en la ingesta de PEDIDO, que es lo cómodo para depurar.
	if s.cola != nil && r.URL.Query().Get("sync") != "1" {
		err := s.cola.Encolar(r.Context(), cola.Trabajo{
			FuenteID:        p.FuenteID,
			FolderID:        p.FolderID,
			DriveFileID:     p.DriveFileID,
			Nombre:          p.Nombre,
			RutaCarpeta:     p.RutaCarpeta,
			Creado:          creado,
			ContenidoBase64: p.ContenidoBase64,
		})
		if err != nil {
			s.log.Error("no se pudo encolar", "fichero", p.Nombre, "error", err)
			// 503 y no 400: el fichero está bien, el que falla somos nosotros.
			// Con 503 n8n sabe que tiene que reintentar.
			responderError(w, http.StatusServiceUnavailable, "la cola no está disponible")
			return
		}
		responder(w, http.StatusAccepted, map[string]any{"ok": true, "queued": true})
		return
	}

	nuevo, puntos, err := s.ingesta.Recibir(r.Context(), ingesta.Empujado{
		FuenteID:    p.FuenteID,
		FolderID:    p.FolderID,
		DriveFileID: p.DriveFileID,
		Nombre:      p.Nombre,
		RutaCarpeta: p.RutaCarpeta,
		Creado:      creado,
		Contenido:   contenido,
	})
	if err != nil {
		// 400 y no 500: casi siempre es una carpeta sin dar de alta o un campo
		// que falta, y n8n tiene que poder distinguir "corrígelo" de "reintenta".
		s.log.Warn("empuje rechazado", "fichero", p.Nombre, "error", err)
		responderError(w, http.StatusBadRequest, err.Error())
		return
	}

	responder(w, http.StatusOK, map[string]any{
		"ok":     true,
		"added":  nuevo,
		"points": puntos,
	})
}

// claveDeServicioValida compara en tiempo constante, para no filtrar la clave
// carácter a carácter con la duración de la respuesta.
func (s *Servidor) claveDeServicioValida(r *http.Request) bool {
	esperada := s.cfg.ClaveServicio
	if esperada == "" {
		return false // sin clave configurada, la puerta está cerrada
	}
	recibida := r.Header.Get("x-api-key")
	return subtle.ConstantTimeCompare([]byte(recibida), []byte(esperada)) == 1
}

// estadoCola es lo que enseña la pantalla de administración: cuántos ficheros
// esperan, cuántos se están procesando y cuántos se apartaron.
func (s *Servidor) estadoCola(w http.ResponseWriter, r *http.Request) {
	if s.cola == nil {
		responder(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	e, err := s.cola.Estado(r.Context())
	if err != nil {
		s.fallo(w, "estado de la cola", err)
		return
	}
	responder(w, http.StatusOK, map[string]any{
		"active":     true,
		"pending":    e.Pendientes,
		"processing": e.Procesando,
		"failed":     e.Fallidos,
	})
}
