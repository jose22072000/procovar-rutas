package ingesta

import (
	"testing"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
)

func gpxCon(puntos string) []byte {
	return []byte(`<?xml version="1.0"?><gpx version="1.1" creator="GPS Logger">
<trk><name>ALEXANDER</name><trkseg>` + puntos + `</trkseg></trk></gpx>`)
}

func entorno() Entorno {
	zona, _ := time.LoadLocation("America/Havana")
	return Entorno{
		TipoFuente: gpx.FuenteSucursal,
		Alias:      map[string]string{gpx.Normalizar("Alexander"): "t-alex"},
		Zona:       zona,
	}
}

func TestExaminarFicheroBueno(t *testing.T) {
	f := drive.Fichero{ID: "d1", Nombre: "ruta.gpx"}
	v := Examinar(f, gpxCon(
		`<trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt>`+
			`<trkpt lat="21.39" lon="-77.92"><time>2026-08-10T14:00:00Z</time></trkpt>`), entorno())

	if v.Estado != EstadoProcesado {
		t.Fatalf("estado = %s (%s)", v.Estado, v.Error)
	}
	if v.TrabajadorID != "t-alex" || v.Via != gpx.ViaGpx {
		t.Errorf("resolución = %s vía %s", v.TrabajadorID, v.Via)
	}
	// 13:00 UTC son las 09:00 en Cuba: el día local es el 10, no el 11.
	if v.Fecha != "2026-08-10" || v.OrigenFecha != FechaDePuntos {
		t.Errorf("fecha = %s (%s)", v.Fecha, v.OrigenFecha)
	}
}

// Un fix de la madrugada UTC pertenece al día ANTERIOR en Cuba. Es el error
// clásico de este tipo de sistema, y el que haría que un lunes apareciera como
// domingo en el calendario.
func TestExaminarFechaLocalNoUTC(t *testing.T) {
	f := drive.Fichero{ID: "d1", Nombre: "ruta.gpx"}
	v := Examinar(f, gpxCon(
		`<trkpt lat="21.38" lon="-77.91"><time>2026-08-11T02:00:00Z</time></trkpt>`), entorno())

	if v.Fecha != "2026-08-10" {
		t.Errorf("fecha = %s; las 02:00 UTC del 11 son las 22:00 del 10 en Cuba", v.Fecha)
	}
}

func TestExaminarSinAsignar(t *testing.T) {
	f := drive.Fichero{ID: "d1", Nombre: "track_001.gpx"}
	datos := []byte(`<?xml version="1.0"?><gpx version="1.1" creator="Redmi Note 12">
<trk><trkseg><trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt></trkseg></trk></gpx>`)
	v := Examinar(f, datos, entorno())

	if v.Estado != EstadoSinAsignar {
		t.Errorf("estado = %s", v.Estado)
	}
	if v.PistaAlias != "Redmi Note 12" {
		t.Errorf("pista = %q; es lo que el admin tiene que casar en la bandeja", v.PistaAlias)
	}
	// Aunque no se sepa de quién es, la fecha ya se conoce: cuando se asigne, el
	// día se calcula sin volver a bajar el fichero.
	if v.Fecha != "2026-08-10" {
		t.Errorf("fecha = %s", v.Fecha)
	}
}

func TestExaminarSinHorasCaeAlNombre(t *testing.T) {
	f := drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}
	v := Examinar(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"></trkpt>`), entorno())

	if v.Fecha != "2026-08-10" || v.OrigenFecha != FechaDeNombre {
		t.Errorf("fecha = %s (%s)", v.Fecha, v.OrigenFecha)
	}
	// Se conoce el día, así que es procesable; el día saldrá SIN_FECHA porque no
	// hay horas con las que medir la jornada.
	if v.Estado != EstadoProcesado {
		t.Errorf("estado = %s", v.Estado)
	}
}

func TestExaminarSinHorasNiNombreUsaLaFechaDeDrive(t *testing.T) {
	creado := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	f := drive.Fichero{ID: "d1", Nombre: "alexander.gpx", Creado: creado}
	v := Examinar(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"></trkpt>`), entorno())

	if v.OrigenFecha != FechaDeDrive {
		t.Errorf("origen = %s; debería marcarse como fecha inferida", v.OrigenFecha)
	}
	if v.Fecha != "2026-08-10" {
		t.Errorf("fecha = %s", v.Fecha)
	}
}

func TestExaminarFicheroRotoNoSePierde(t *testing.T) {
	f := drive.Fichero{ID: "d1", Nombre: "roto.gpx"}
	v := Examinar(f, []byte("esto no es xml"), entorno())

	if v.Estado != EstadoError || v.Error == "" {
		t.Errorf("= %+v", v)
	}
	if v.PistaAlias != "roto.gpx" {
		t.Errorf("pista = %q; hace falta para encontrarlo en la bandeja", v.PistaAlias)
	}
}

func TestExaminarCarpetaDeVendedorManda(t *testing.T) {
	ent := entorno()
	ent.TipoFuente = gpx.FuenteVendedor
	ent.TrabajadorIDFuente = "t-yas"

	f := drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}
	v := Examinar(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt>`), ent)

	if v.TrabajadorID != "t-yas" {
		t.Errorf("en una carpeta de vendedor manda la carpeta, no el nombre: %s", v.TrabajadorID)
	}
}
