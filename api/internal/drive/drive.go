// Package drive reads the Google Drive folders where sellers upload their .gpx
// files.
//
// Ingest talks to the Client INTERFACE, not to Google. Two reasons, and the second
// is the one that matters:
//
//  1. Ingest is tested end to end with no credentials and no network (see fake.go).
//  2. The day the sales-force app sends positions over an API, the source changes
//     and the rest of the system — parser, metrics, panel, report — stays exactly
//     as it is.
package drive

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// File is a .gpx seen in a folder.
type File struct {
	ID string
	// Name includes the extension.
	Name string
	// FolderPath is the sub-folders from the source root, outermost first. It is
	// what allows inferring the seller when the tree says so.
	FolderPath []string
	Size       int64
	Created    time.Time
	Modified   time.Time
}

// Client is what ingest needs from Drive. Nothing more.
type Client interface {
	// List returns the .gpx files in the folder and its sub-folders. When `since`
	// is non-zero, only those modified after it (incremental scan).
	List(ctx context.Context, folderID string, desde time.Time, max int) ([]File, error)
	// Download fetches a file's contents.
	Download(ctx context.Context, fileID string) ([]byte, error)
}

type clienteGoogle struct {
	svc *drive.Service
}

// New builds a Google Drive client from a service account.
//
// The scope is READ-ONLY on purpose: this system never moves or deletes anything
// files in the sellers' Drive, and it is better that it cannot even try.
func New(ctx context.Context, credencialJSON []byte) (Client, error) {
	svc, err := drive.NewService(ctx,
		option.WithCredentialsJSON(credencialJSON),
		option.WithScopes(drive.DriveReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir Drive: %w", err)
	}
	return &clienteGoogle{svc: svc}, nil
}

// folder still to be walked.
type pendiente struct {
	id   string
	ruta []string
}

const mimeCarpeta = "application/vnd.google-apps.folder"

func (c *clienteGoogle) List(ctx context.Context, folderID string, desde time.Time, max int) ([]File, error) {
	ficheros := []File{}
	cola := []pendiente{{id: folderID, ruta: nil}}
	visitadas := map[string]bool{}

	for len(cola) > 0 && len(ficheros) < max {
		actual := cola[0]
		cola = cola[1:]
		if visitadas[actual.id] {
			// A Drive shortcut can create a cycle. Without this the scan would go
			// round for ever.
			continue
		}
		visitadas[actual.id] = true

		consulta := fmt.Sprintf("'%s' in parents and trashed = false", actual.id)
		if !desde.IsZero() {
			// Only the incremental scan filters by date. The nightly sweep passes a
			// zero `since` and walks everything, which is what guarantees nothing is
			// missing even if a file arrives renamed or with a changed date.
			consulta += fmt.Sprintf(" and modifiedTime > '%s'", desde.UTC().Format(time.RFC3339))
		}

		llamada := c.svc.Files.List().
			Q(consulta).
			Fields("nextPageToken, files(id, name, mimeType, size, createdTime, modifiedTime)").
			PageSize(1000).
			SupportsAllDrives(true).
			IncludeItemsFromAllDrives(true).
			Context(ctx)

		for {
			res, err := llamada.Do()
			if err != nil {
				return ficheros, fmt.Errorf("listando %s: %w", actual.id, err)
			}
			for _, f := range res.Files {
				if f.MimeType == mimeCarpeta {
					// Sub-folders are ALWAYS walked, even in the incremental scan: a
					// folder's date does not change when someone drops a file into
					// it, so filtering folders by
					// modifiedTime escondería ficheros nuevos.
					cola = append(cola, pendiente{id: f.Id, ruta: append(append([]string{}, actual.ruta...), f.Name)})
					continue
				}
				if !isGpx(f.Name) {
					continue
				}
				ficheros = append(ficheros, File{
					ID:         f.Id,
					Name:       f.Name,
					FolderPath: actual.ruta,
					Size:       f.Size,
					Created:    parseHora(f.CreatedTime),
					Modified:   parseHora(f.ModifiedTime),
				})
			}
			if res.NextPageToken == "" || len(ficheros) >= max {
				break
			}
			llamada = llamada.PageToken(res.NextPageToken)
		}
	}

	return ficheros, nil
}

func (c *clienteGoogle) Download(ctx context.Context, fileID string) ([]byte, error) {
	res, err := c.svc.Files.Get(fileID).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("descargando %s: %w", fileID, err)
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// isGpx filters by NAME and not by mimeType on purpose: Drive serves .gpx files
// como application/octet-stream, como text/xml o como application/gpx+xml según
// they were uploaded, and filtering by type would leave half of them out.
func isGpx(nombre string) bool {
	return strings.HasSuffix(strings.ToLower(nombre), ".gpx")
}

func parseHora(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
