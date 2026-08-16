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
		SourceType: gpx.SourceBranch,
		Alias:      map[string]string{gpx.Normalize("Alexander"): "t-alex"},
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
	// 13:00 UTC is 09:00 in Cuba: the local day is the 10th, not the 11th.
	if v.Date != "2026-08-10" || v.DateSource != DateFromPoints {
		t.Errorf("date = %s (%s)", v.Date, v.DateSource)
	}
}

// A fix from the small hours UTC belongs to the PREVIOUS day in Cuba. It is the
// classic bug in this kind of system, and the one that would make a Monday show
// up as a Sunday on the calendar.
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
	// Even when the owner is unknown the date is already known: once assigned, the
	// day is computed without downloading the file again.
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
	// The day is known, so it is processable; the day will come out SIN_FECHA because
	// there are no times to measure the workday with.
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
	ent.SourceType = gpx.SourceSeller
	ent.SourceSellerID = "t-yas"

	f := drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}
	v := Examine(f, gpxCon(`<trkpt lat="21.38" lon="-77.91"><time>2026-08-10T13:00:00Z</time></trkpt>`), ent)

	if v.SellerID != "t-yas" {
		t.Errorf("en una carpeta de vendedor manda la carpeta, no el nombre: %s", v.SellerID)
	}
}

// El nombre de la sucursal sale de la cuenta que comparte la carpeta, y esas
// cuentas están escritas de varias formas.
func TestNombreDeCuenta(t *testing.T) {
	casos := map[string]string{
		"habanaprocovar@gmail.com":    "habana",
		"camaguey.procovar@gmail.com": "camaguey",
		"Holguin.Procovar@gmail.com":  "Holguin",
		"tablets.procovar@gmail.com":  "tablets",
		"granma":                      "granma",
		"":                            "",
	}
	for entra, espera := range casos {
		if sale := nombreDeCuenta(entra); sale != espera {
			t.Errorf("nombreDeCuenta(%q) = %q, esperaba %q", entra, sale, espera)
		}
	}
}

// Escriban la cuenta como la escriban, la sucursal tiene que ser LA MISMA.
//
// Esto es lo que se rompió en producción: la misma provincia llegó como "Camagüey
// Procovar" desde el nombre visible y como "camaguey.procovar@gmail.com" desde el
// correo, y acabaron siendo dos sucursales distintas — con siete "Guantánamo" y
// cuatro "Holguin" por el camino. El nombre se guarda para leerlo; lo que decide si
// una sucursal ya existe es la clave.
func TestLaMismaCuentaDaLaMismaSucursal(t *testing.T) {
	grupos := [][]string{
		{"Camagüey Procovar", "camaguey.procovar@gmail.com", "CAMAGUEY", "camagüey"},
		{"Holguín Procovar", "Holguin.Procovar@gmail.com", "holguinprocovar"},
		{"Las Tunas Procovar", "lastunasprocovar@gmail.com", "LAS TUNAS"},
		{"Santiago Procovar", "santiagoprocovar@gmail.com", "santiago"},
	}
	for _, formas := range grupos {
		esperada := claveDeSucursal(nombreDeCuenta(formas[0]))
		if esperada == "" {
			t.Fatalf("clave vacía para %q", formas[0])
		}
		for _, f := range formas[1:] {
			if k := claveDeSucursal(nombreDeCuenta(f)); k != esperada {
				t.Errorf("%q da %q y %q da %q: serían dos sucursales", formas[0], esperada, f, k)
			}
		}
	}
}
