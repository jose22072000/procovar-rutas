package drive

import (
	"context"
	"fmt"
	"time"
)

// Falso es un Drive en memoria para las pruebas y para el modo demostración.
//
// Permite ejercitar la ingesta entera —resolución de vendedor, deduplicación por
// hash, ficheros sin fecha, errores de descarga— sin credenciales, sin red y sin
// esperar a que lleguen las carpetas reales.
type Falso struct {
	// Ficheros por ID de carpeta.
	Carpetas map[string][]File
	// Content por ID de fichero.
	Content map[string][]byte
	// Fallos fuerza un error al descargar ese fichero, para probar que un
	// fichero malo no tumba el barrido entero.
	Fallos map[string]error
	// Llamadas cuenta las descargas: sirve para comprobar que un fichero ya
	// procesado NO se vuelve a bajar.
	Llamadas int
}

func NuevoFalso() *Falso {
	return &Falso{
		Carpetas: map[string][]File{},
		Content:  map[string][]byte{},
		Fallos:   map[string]error{},
	}
}

// Agregar mete un fichero en una carpeta.
func (f *Falso) Agregar(folderID string, fich File, contenido []byte) {
	if fich.Modified.IsZero() {
		fich.Modified = time.Now().UTC()
	}
	if fich.Created.IsZero() {
		fich.Created = fich.Modified
	}
	fich.Size = int64(len(contenido))
	f.Carpetas[folderID] = append(f.Carpetas[folderID], fich)
	f.Content[fich.ID] = contenido
}

func (f *Falso) Listar(_ context.Context, folderID string, desde time.Time, max int) ([]File, error) {
	out := []File{}
	for _, fich := range f.Carpetas[folderID] {
		if !desde.IsZero() && !fich.Modified.After(desde) {
			continue
		}
		if len(out) >= max {
			break
		}
		out = append(out, fich)
	}
	return out, nil
}

func (f *Falso) Descargar(_ context.Context, fileID string) ([]byte, error) {
	f.Llamadas++
	if err, mal := f.Fallos[fileID]; mal {
		return nil, err
	}
	datos, ok := f.Content[fileID]
	if !ok {
		return nil, fmt.Errorf("fichero %s no encontrado", fileID)
	}
	return datos, nil
}
