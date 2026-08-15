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
// The built-in scan pulls .gpx files from Drive, but Procovar already runs an n8n
// that watches the folders and works — it is how orders come in — so ingest also
// accepts the opposite direction: n8n sends the file and all that happens here is
// procese.
//
// Both routes end in the same place and are idempotent on `driveFileId` and on
// `sha256`, so they can coexist without duplicating anything: if n8n already put
// today's file in, tonight's scan will see it and skip it.

// Pushed is what n8n sends.
type Pushed struct {
	// SourceID or FolderID identify the folder already registered.
	SourceID string
	FolderID string
	// DriveFileID is the deduplication key: the same file sent twice does not go
	// in twice.
	DriveFileID string
	Name        string
	// FolderPath is the sub-folders inside the source. It is where the seller comes
	// from when the file name only carries the date.
	FolderPath []string
	Created    time.Time
	Content    []byte
}

// Receive processes a pushed file.
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

// findSource locates the folder the file belongs to.
//
// It is not created on the fly when missing: an unknown folder is almost always a
// misconfiguration in n8n, and creating it silently would make files show up under
// a phantom source with no branch and no time zone.
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
