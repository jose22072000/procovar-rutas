package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Ficheros empujados desde fuera.
//
// El barrido propio baja los .gpx de Drive, pero en Procovar ya hay un n8n que
// vigila las carpetas y funciona —es como entran los pedidos—, así que la ingesta
// admite también el camino contrario: que n8n mande el file y aquí solo se
// procese.
//
// Los dos caminos acaban en el mismo sitio y son idempotentes por `driveFileId`
// y por `sha256`, de modo que pueden convivir sin duplicar nada: si n8n ya metió
// el file de hoy, el barrido de esta noche lo verá y lo saltará.

// Pushed es lo que manda n8n.
type Pushed struct {
	// SourceID o FolderID identifican la carpeta ya dada de alta.
	SourceID string
	FolderID string
	// DriveFileID es la clave de deduplicación: el mismo file mandado dos
	// veces no entra dos veces.
	DriveFileID string
	Name        string
	// FolderPath son las subcarpetas dentro de la source. Es de donde sale el
	// vendedor cuando el nombre del file solo trae la date.
	FolderPath []string
	Created    time.Time
	Content    []byte
}

// Receive procesa un file empujado.
func (s *Service) Receive(ctx context.Context, e Pushed) (bool, int64, error) {
	if e.DriveFileID == "" || e.Name == "" || len(e.Content) == 0 {
		return false, 0, fmt.Errorf("faltan el identificador, el nombre o el contenido")
	}

	source, err := s.findSource(ctx, e)
	if err != nil {
		return false, 0, err
	}

	alias, err := s.aliasMap(ctx)
	if err != nil {
		return false, 0, err
	}
	zona := s.sourceZone(ctx, source)

	f := drive.File{
		ID:         e.DriveFileID,
		Name:       e.Name,
		FolderPath: e.FolderPath,
		Size:       int64(len(e.Content)),
		Created:    e.Created,
		Modified:   e.Created,
	}

	diasTocados := map[claveDia]bool{}
	nuevo, points, err := s.Save(ctx, source, f, e.Content, alias, zona, diasTocados)
	if err != nil {
		return false, 0, err
	}

	for d := range diasTocados {
		if err := s.RecomputeDay(ctx, d.trabajador, d.date); err != nil {
			s.log.Error("recálculo tras empuje", "trabajador", d.trabajador, "error", err)
		}
	}

	return nuevo, points, nil
}

// findSource localiza la carpeta a la que pertenece el file.
//
// No se crea sola si no existe: una carpeta desconocida es casi siempre un error
// de configuración en n8n, y crearla en silencio haría que los ficheros
// aparecieran bajo una source fantasma sin sucursal ni zona horaria.
func (s *Service) findSource(ctx context.Context, e Pushed) (store.DriveSource, error) {
	if e.SourceID != "" {
		f, err := s.q.SourceByID(ctx, e.SourceID)
		if err != nil {
			return store.DriveSource{}, fmt.Errorf("la source %s no existe", e.SourceID)
		}
		return f, nil
	}

	if e.FolderID == "" {
		return store.DriveSource{}, fmt.Errorf("hay que decir de qué carpeta viene (fuenteId o folderId)")
	}

	fuentes, err := s.q.ActiveSources(ctx)
	if err != nil {
		return store.DriveSource{}, err
	}
	for _, f := range fuentes {
		if f.FolderID == e.FolderID {
			return f, nil
		}
	}
	return store.DriveSource{}, fmt.Errorf(
		"la carpeta %s no está dada de alta en Administración", e.FolderID)
}
