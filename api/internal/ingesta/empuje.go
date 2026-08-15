package ingesta

import (
	"context"
	"fmt"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/drive"
)

// Ficheros empujados desde fuera.
//
// El barrido propio baja los .gpx de Drive, pero en Procovar ya hay un n8n que
// vigila las carpetas y funciona —es como entran los pedidos—, así que la ingesta
// admite también el camino contrario: que n8n mande el fichero y aquí solo se
// procese.
//
// Los dos caminos acaban en el mismo sitio y son idempotentes por `driveFileId`
// y por `sha256`, de modo que pueden convivir sin duplicar nada: si n8n ya metió
// el fichero de hoy, el barrido de esta noche lo verá y lo saltará.

// Empujado es lo que manda n8n.
type Empujado struct {
	// FuenteID o FolderID identifican la carpeta ya dada de alta.
	FuenteID string
	FolderID string
	// DriveFileID es la clave de deduplicación: el mismo fichero mandado dos
	// veces no entra dos veces.
	DriveFileID string
	Nombre      string
	// RutaCarpeta son las subcarpetas dentro de la fuente. Es de donde sale el
	// vendedor cuando el nombre del fichero solo trae la fecha.
	RutaCarpeta []string
	Creado      time.Time
	Contenido   []byte
}

// Recibir procesa un fichero empujado.
func (s *Servicio) Recibir(ctx context.Context, e Empujado) (bool, int64, error) {
	if e.DriveFileID == "" || e.Nombre == "" || len(e.Contenido) == 0 {
		return false, 0, fmt.Errorf("faltan el identificador, el nombre o el contenido")
	}

	fuente, err := s.buscarFuente(ctx, e)
	if err != nil {
		return false, 0, err
	}

	alias, err := s.mapaAlias(ctx)
	if err != nil {
		return false, 0, err
	}
	zona := s.zonaDeFuente(ctx, fuente)

	f := drive.Fichero{
		ID:          e.DriveFileID,
		Nombre:      e.Nombre,
		RutaCarpeta: e.RutaCarpeta,
		Tamano:      int64(len(e.Contenido)),
		Creado:      e.Creado,
		Modificado:  e.Creado,
	}

	diasTocados := map[claveDia]bool{}
	nuevo, puntos, err := s.Guardar(ctx, fuente, f, e.Contenido, alias, zona, diasTocados)
	if err != nil {
		return false, 0, err
	}

	for d := range diasTocados {
		if err := s.RecalcularDia(ctx, d.trabajador, d.fecha); err != nil {
			s.log.Error("recálculo tras empuje", "trabajador", d.trabajador, "error", err)
		}
	}

	return nuevo, puntos, nil
}

// buscarFuente localiza la carpeta a la que pertenece el fichero.
//
// No se crea sola si no existe: una carpeta desconocida es casi siempre un error
// de configuración en n8n, y crearla en silencio haría que los ficheros
// aparecieran bajo una fuente fantasma sin sucursal ni zona horaria.
func (s *Servicio) buscarFuente(ctx context.Context, e Empujado) (almacen.DriveSource, error) {
	if e.FuenteID != "" {
		f, err := s.q.FuentePorID(ctx, e.FuenteID)
		if err != nil {
			return almacen.DriveSource{}, fmt.Errorf("la fuente %s no existe", e.FuenteID)
		}
		return f, nil
	}

	if e.FolderID == "" {
		return almacen.DriveSource{}, fmt.Errorf("hay que decir de qué carpeta viene (fuenteId o folderId)")
	}

	fuentes, err := s.q.FuentesActivas(ctx)
	if err != nil {
		return almacen.DriveSource{}, err
	}
	for _, f := range fuentes {
		if f.FolderID == e.FolderID {
			return f, nil
		}
	}
	return almacen.DriveSource{}, fmt.Errorf(
		"la carpeta %s no está dada de alta en Administración", e.FolderID)
}
