// Package ingesta baja los .gpx de Drive, los mete en la base y recalcula el
// día de cada vendedor.
//
// Este fichero contiene la parte que DECIDE —de quién es un fichero, de qué día
// es, si sirve— separada de la que escribe en la base. La decisión es una
// función pura y se prueba entera sin Postgres ni credenciales de Google; lo
// otro es escribir filas.
package ingesta

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
)

// Estado de un fichero tras examinarlo. Se corresponde con el enum
// estado_fichero de la base.
type Estado string

const (
	EstadoProcesado  Estado = "PROCESADO"
	EstadoSinAsignar Estado = "SIN_ASIGNAR"
	EstadoSinFecha   Estado = "SIN_FECHA"
	EstadoError      Estado = "ERROR"
)

// OrigenFecha dice de dónde salió la fecha del día. Un día fechado por el nombre
// del fichero no vale lo mismo que uno fechado por sus propios puntos, y el
// panel lo enseña.
type OrigenFecha string

const (
	FechaDePuntos OrigenFecha = "PUNTOS"
	FechaDeNombre OrigenFecha = "NOMBRE"
	FechaDeDrive  OrigenFecha = "DRIVE"
	FechaNinguna  OrigenFecha = "NINGUNO"
)

// Veredicto es lo que se sabe de un fichero antes de tocar la base.
type Veredicto struct {
	Estado       Estado
	Error        string
	TrabajadorID string
	Via          gpx.Via
	// PistaAlias es el texto que se le enseñará al admin en la bandeja.
	PistaAlias string
	// Fecha es el día local, "YYYY-MM-DD". Vacío si no se pudo fechar.
	Fecha       string
	OrigenFecha OrigenFecha
	Parseado    *gpx.Parseado
}

// Entorno es lo que hace falta saber para juzgar un fichero.
type Entorno struct {
	TipoFuente         gpx.TipoFuente
	TrabajadorIDFuente string
	// NombreFuente es el nombre de la carpeta dada de alta.
	NombreFuente string
	// Alias normalizado -> trabajadorID.
	Alias map[string]string
	// Zona horaria de la sucursal, para pasar de instante UTC a día local.
	Zona *time.Location
}

// Examinar decide qué hacer con un fichero recién bajado.
//
// Nunca devuelve error: un fichero que no se puede leer, o del que no se sabe
// de quién es, NO se descarta — se registra con su estado y su pista para que
// aparezca en la bandeja. Ningún fichero se pierde en silencio.
func Examinar(f drive.Fichero, datos []byte, ent Entorno) Veredicto {
	parseado, err := gpx.Parse(datos)
	if err != nil {
		return Veredicto{
			Estado:      EstadoError,
			Error:       err.Error(),
			PistaAlias:  f.Nombre,
			OrigenFecha: FechaNinguna,
		}
	}

	res := gpx.ResolverTrabajador(gpx.Contexto{
		TipoFuente:         ent.TipoFuente,
		TrabajadorIDFuente: ent.TrabajadorIDFuente,
		NombreFuente:       ent.NombreFuente,
		RutaCarpeta:        f.RutaCarpeta,
		NombreFichero:      f.Nombre,
		PistasGpx:          parseado.Pistas,
		Alias:              ent.Alias,
	})

	v := Veredicto{
		TrabajadorID: res.TrabajadorID,
		Via:          res.Via,
		PistaAlias:   res.Pista,
		Parseado:     parseado,
	}
	if v.PistaAlias == "" {
		v.PistaAlias = f.Nombre
	}

	// La fecha, por orden de fiabilidad.
	zona := ent.Zona
	if zona == nil {
		zona = time.UTC
	}
	switch {
	case parseado.PrimerFix != nil:
		v.Fecha = parseado.PrimerFix.In(zona).Format("2006-01-02")
		v.OrigenFecha = FechaDePuntos
	case gpx.FechaDelNombre(f.Nombre) != "":
		v.Fecha = gpx.FechaDelNombre(f.Nombre)
		v.OrigenFecha = FechaDeNombre
	case !f.Creado.IsZero():
		// La más floja: la fecha de subida a Drive. Puede ser días posterior al
		// recorrido, así que el día queda marcado como inferido y el panel lo
		// pinta distinto en vez de dar por bueno lo que no lo es.
		v.Fecha = f.Creado.In(zona).Format("2006-01-02")
		v.OrigenFecha = FechaDeDrive
	default:
		v.OrigenFecha = FechaNinguna
	}

	switch {
	case res.TrabajadorID == "":
		v.Estado = EstadoSinAsignar
	case v.Fecha == "":
		v.Estado = EstadoSinFecha
	default:
		v.Estado = EstadoProcesado
	}

	return v
}
