package gpx

import (
	"fmt"
	"strings"
	"testing"
)

func doc(cuerpo string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="GPS Logger" xmlns="http://www.topografix.com/GPX/1/1">` + cuerpo + `</gpx>`)
}

func pt(lat, lon float64, hora, extra string) string {
	t := ""
	if hora != "" {
		t = "<time>" + hora + "</time>"
	}
	return fmt.Sprintf(`<trkpt lat="%v" lon="%v">%s%s</trkpt>`, lat, lon, t, extra)
}

func TestParsePistaNormal(t *testing.T) {
	r, err := Parse(doc(`<trk><name>ALEXANDER</name><trkseg>` +
		pt(21.38, -77.91, "2026-08-10T13:00:00Z", "") +
		pt(21.39, -77.92, "2026-08-10T13:10:00Z", "") +
		`</trkseg></trk>`))
	if err != nil {
		t.Fatalf("no debería fallar: %v", err)
	}
	if len(r.Points) != 2 {
		t.Errorf("puntos = %d, se esperaban 2", len(r.Points))
	}
	if r.NoTime != 0 {
		t.Errorf("sinHora = %d, se esperaba 0", r.NoTime)
	}
	if r.FirstFix == nil || r.FirstFix.Format("15:04") != "13:00" {
		t.Errorf("primerFix = %v", r.FirstFix)
	}
	if !contiene(r.Hints, "ALEXANDER") || !contiene(r.Hints, "GPS Logger") {
		t.Errorf("pistas = %v, faltan el nombre de la pista o el creador", r.Hints)
	}
}

func TestParseVariosTrkYTrkseg(t *testing.T) {
	r, err := Parse(doc(
		`<trk><trkseg>` + pt(21.1, -77.1, "2026-08-10T13:00:00Z", "") + `</trkseg>` +
			`<trkseg>` + pt(21.2, -77.2, "2026-08-10T14:00:00Z", "") + `</trkseg></trk>` +
			`<trk><trkseg>` + pt(21.3, -77.3, "2026-08-10T15:00:00Z", "") + `</trkseg></trk>`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(r.Points) != 3 {
		t.Fatalf("puntos = %d, se esperaban 3", len(r.Points))
	}
	for i, p := range r.Points {
		if p.Seq != i {
			t.Errorf("seq[%d] = %d", i, p.Seq)
		}
	}
}

// Un fichero sin horas no se descarta: se puede pintar el recorrido, pero no
// hay jornada que medir. Es uno de los tres casos que hay que cazar.
func TestParseSinHoras(t *testing.T) {
	r, err := Parse(doc(`<trk><trkseg>` + pt(21.1, -77.1, "", "") + pt(21.2, -77.2, "", "") + `</trkseg></trk>`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(r.Points) != 2 || r.NoTime != 2 {
		t.Errorf("puntos = %d, sinHora = %d", len(r.Points), r.NoTime)
	}
	if r.FirstFix != nil {
		t.Errorf("no debería haber primer fix: %v", r.FirstFix)
	}
}

func TestParseDescartaCoordenadasImposibles(t *testing.T) {
	r, err := Parse(doc(`<trk><trkseg>` +
		pt(21.38, -77.91, "2026-08-10T13:00:00Z", "") +
		pt(0, 0, "2026-08-10T13:01:00Z", "") + // isla nula: fix sin señal
		pt(999, -77.9, "2026-08-10T13:02:00Z", "") +
		pt(21.39, -77.92, "2026-08-10T13:03:00Z", "") +
		`</trkseg></trk>`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(r.Points) != 2 {
		t.Errorf("puntos = %d, se esperaban 2 (el fichero sigue siendo válido)", len(r.Points))
	}
}

func TestParseOrdenaSesionesDesordenadas(t *testing.T) {
	r, err := Parse(doc(`<trk><trkseg>` +
		pt(21.3, -77.3, "2026-08-10T15:00:00Z", "") +
		pt(21.1, -77.1, "2026-08-10T13:00:00Z", "") +
		`</trkseg></trk>`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if r.FirstFix.Format("15:04") != "13:00" || r.LastFix.Format("15:04") != "15:00" {
		t.Errorf("primer=%v ultimo=%v", r.FirstFix, r.LastFix)
	}
}

func TestParsePrecision(t *testing.T) {
	r, err := Parse(doc(`<trk><trkseg>` +
		pt(21.1, -77.1, "2026-08-10T13:00:00Z", "<hdop>2</hdop>") +
		pt(21.2, -77.2, "2026-08-10T13:01:00Z", "<extensions><accuracy>12</accuracy></extensions>") +
		`</trkseg></trk>`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if r.Points[0].Accuracy == nil || *r.Points[0].Accuracy != 10 {
		t.Errorf("hdop 2 debería dar 10 m, dio %v", r.Points[0].Accuracy)
	}
	if r.Points[1].Accuracy == nil || *r.Points[1].Accuracy != 12 {
		t.Errorf("accuracy = %v", r.Points[1].Accuracy)
	}
}

func TestParseFicherosInservibles(t *testing.T) {
	if _, err := Parse(doc(`<trk><trkseg></trkseg></trk>`)); err == nil {
		t.Error("un fichero sin puntos debería dar error")
	}
	if _, err := Parse([]byte("esto no es xml")); err == nil {
		t.Error("un XML roto debería dar error")
	}
}

func TestParseWaypointsSueltos(t *testing.T) {
	r, err := Parse([]byte(`<gpx version="1.1"><wpt lat="21.1" lon="-77.1"><time>2026-08-10T13:00:00Z</time></wpt></gpx>`))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(r.Points) != 1 {
		t.Errorf("puntos = %d", len(r.Points))
	}
}

func contiene(xs []string, s string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
