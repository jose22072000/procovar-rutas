// Package drive lee las carpetas de Google Drive donde los vendedores suben sus
// .gpx.
//
// La ingesta habla con la INTERFAZ Cliente, no con Google. Dos motivos, y el
// segundo es el importante:
//
//  1. La ingesta se prueba entera sin credenciales ni red (ver falso.go).
//  2. El día que la APK de fuerza de ventas mande las posiciones por API, la
//     fuente cambia y el resto del sistema —parser, métricas, panel, reporte—
//     se queda igual.
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

// File es un .gpx visto en una carpeta.
type File struct {
	ID string
	// Name incluye la extensión.
	Name string
	// FolderPath son las subcarpetas desde la raíz de la fuente, de fuera a
	// dentro. Es lo que permite deducir el vendedor cuando el árbol lo dice.
	FolderPath []string
	Size       int64
	Created    time.Time
	Modified   time.Time
}

// Cliente es lo que la ingesta necesita de Drive. Nada más.
type Cliente interface {
	// Listar devuelve los .gpx de la carpeta y de sus subcarpetas. Si `desde` no
	// es cero, solo los modificados después (barrido incremental).
	Listar(ctx context.Context, folderID string, desde time.Time, max int) ([]File, error)
	// Descargar trae el contenido de un fichero.
	Descargar(ctx context.Context, fileID string) ([]byte, error)
}

type clienteGoogle struct {
	svc *drive.Service
}

// Nuevo crea un cliente contra Google Drive con una service account.
//
// El ámbito es de SOLO LECTURA a propósito: este sistema nunca mueve ni borra
// ficheros del Drive de los trabajadores, y conviene que ni siquiera pueda.
func Nuevo(ctx context.Context, credencialJSON []byte) (Cliente, error) {
	svc, err := drive.NewService(ctx,
		option.WithCredentialsJSON(credencialJSON),
		option.WithScopes(drive.DriveReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir Drive: %w", err)
	}
	return &clienteGoogle{svc: svc}, nil
}

// carpeta pendiente de recorrer.
type pendiente struct {
	id   string
	ruta []string
}

const mimeCarpeta = "application/vnd.google-apps.folder"

func (c *clienteGoogle) Listar(ctx context.Context, folderID string, desde time.Time, max int) ([]File, error) {
	ficheros := []File{}
	cola := []pendiente{{id: folderID, ruta: nil}}
	visitadas := map[string]bool{}

	for len(cola) > 0 && len(ficheros) < max {
		actual := cola[0]
		cola = cola[1:]
		if visitadas[actual.id] {
			// Un atajo de Drive puede crear un ciclo. Sin esto, el barrido daría
			// vueltas para siempre.
			continue
		}
		visitadas[actual.id] = true

		consulta := fmt.Sprintf("'%s' in parents and trashed = false", actual.id)
		if !desde.IsZero() {
			// Solo se filtra por fecha en el incremental. El repaso nocturno pasa
			// `desde` en cero y recorre todo, que es lo que garantiza que no falte
			// nada aunque un fichero llegue con la fecha cambiada o renombrado.
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
					// Las subcarpetas se recorren SIEMPRE, incluso en el
					// incremental: una carpeta nueva no cambia de fecha cuando
					// alguien mete un fichero dentro, así que filtrar carpetas por
					// modifiedTime escondería ficheros nuevos.
					cola = append(cola, pendiente{id: f.Id, ruta: append(append([]string{}, actual.ruta...), f.Name)})
					continue
				}
				if !esGpx(f.Name) {
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

func (c *clienteGoogle) Descargar(ctx context.Context, fileID string) ([]byte, error) {
	res, err := c.svc.Files.Get(fileID).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, fmt.Errorf("descargando %s: %w", fileID, err)
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// esGpx filtra por NOMBRE y no por mimeType a propósito: Drive sirve los .gpx
// como application/octet-stream, como text/xml o como application/gpx+xml según
// cómo se subieran, y filtrar por tipo se dejaría la mitad fuera.
func esGpx(nombre string) bool {
	return strings.HasSuffix(strings.ToLower(nombre), ".gpx")
}

func parseHora(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
