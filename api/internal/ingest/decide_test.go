package ingest

import (
	"testing"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
)

func gpxCon(points string) []byte {
	return []byte(`<?xml version="1.0"?><gpx version="1.1" creator="GPS Logger">
<trk><name>ALEXANDER</name><trkseg>` + points + `</trkseg></trk></gpx>`)
}

func entorno() Env {
	zona, _ := time.LoadLocation("America/Havana")
	return Env{
		SourceType: gpx.FuenteSucursal,
		Alias:      map[string]string{gpx.Normalizar("Alexander"): "t-alex"},
		Zone:       zona,
	}
}

func TestExaminarFicheroBueno(t *testing.T) {
	f := drive.File{ID: "d1", Name: "ruta.gpx"}
	v := Examine(f, gpxCon(
		`<trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt>`+
			`<trkpt lat="21.39" lon="-77.92"><time>2026-08-10T14:00:00Z</time></trkpt>`), entorno())

	if v.Status != StatusProcessed {
		t.Fatalf("status = %s (%s)", v.Status, v.Error)
	}
	if v.SellerID != "t-alex" || v.Via != gpx.ViaGpx {
		t.Errorf("resolución = %s vía %s", v.SellerID, v.Via)
	}
	// 13:00 UTC son las 09:00 en Cuba: el día local es el 10, no el 11.
	if v.Date != "2026-08-10" || v.DateSource != DateFromPoints {
		t.Errorf("date = %s (%s)", v.Date, v.DateSource)
	}
}

// Un fix de la madrugada UTC pertenece al día ANTERIOR en Cuba. Es el error
// clásico de este tipo de sistema, y el que haría que un lunes apareciera como
// domingo en el calendar.
func TestExaminarFechaLocalNoUTC(t *testing.T) {
	f := drive.File{ID: "d1", Name: "ruta.gpx"}
	v := Examine(f, gpxCon(
		`<trkpt lat="21.38" lon="-77.91"><time>2026-08-11T02:00:00Z</time></trkpt>`), entorno())

	if v.Date != "2026-08-10" {
		t.Errorf("date = %s; las 02:00 UTC del 11 son las 22:00 del 10 en Cuba", v.Date)
	}
}

func TestExaminarSinAsignar(t *testing.T) {
	f := drive.File{ID: "d1", Name: "track_001.gpx"}
	datos := []byte(`<?xml version="1.0"?><gpx version="1.1" creator="Redmi Note 12">
<trk><trkseg><trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt></trkseg></trk></gpx>`)
	v := Examine(f, datos, entorno())

	if v.Status != StatusUnassigned {
		t.Errorf("status = %s", v.Status)
	}
	if v.AliasHint != "Redmi Note 12" {
		t.Errorf("pista = %q; es lo que el admin tiene que casar en la bandeja", v.AliasHint)
	}
	// Aunque no se sepa de quién es, la date ya se conoce: cuando se asigne, el
	// día se calcula sin volver a bajar el file.
	if v.Date != "2026-08-10" {
		t.Errorf("date = %s", v.Date)
	}
}

func TestExaminarSinHorasCaeAlNombre(t *testing.T) {
	f := drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}
	v := Examine(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"></trkpt>`), entorno())

	if v.Date != "2026-08-10" || v.DateSource != DateFromName {
		t.Errorf("date = %s (%s)", v.Date, v.DateSource)
	}
	// Se conoce el día, así que es procesable; el día saldrá SIN_FECHA porque no
	// hay horas con las que medir la jornada.
	if v.Status != StatusProcessed {
		t.Errorf("status = %s", v.Status)
	}
}

func TestExaminarSinHorasNiNombreUsaLaDateFromDrive(t *testing.T) {
	creado := time.Date(2026, 8, 10, 18, 0, 0, 0, time.UTC)
	f := drive.File{ID: "d1", Name: "alexander.gpx", Created: creado}
	v := Examine(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"></trkpt>`), entorno())

	if v.DateSource != DateFromDrive {
		t.Errorf("origen = %s; debería marcarse como date inferida", v.DateSource)
	}
	if v.Date != "2026-08-10" {
		t.Errorf("date = %s", v.Date)
	}
}

func TestExaminarFicheroRotoNoSePierde(t *testing.T) {
	f := drive.File{ID: "d1", Name: "roto.gpx"}
	v := Examine(f, []byte("esto no es xml"), entorno())

	if v.Status != StatusFailed || v.Error == "" {
		t.Errorf("= %+v", v)
	}
	if v.AliasHint != "roto.gpx" {
		t.Errorf("pista = %q; hace falta para encontrarlo en la bandeja", v.AliasHint)
	}
}

func TestExaminarCarpetaDeVendedorManda(t *testing.T) {
	ent := entorno()
	ent.SourceType = gpx.FuenteVendedor
	ent.SourceSellerID = "t-yas"

	f := drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}
	v := Examine(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt>`), ent)

	if v.SellerID != "t-yas" {
		t.Errorf("en una carpeta de vendedor manda la carpeta, no el nombre: %s", v.SellerID)
	}
}
