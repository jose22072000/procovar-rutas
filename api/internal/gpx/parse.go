// Package gpx reads the .gpx files sellers upload to the Google Drive folders and
// works out whose each one is and which day it belongs to.
//
// The parser accepts whatever the real loggers export, which is a good deal more
// varied than the official schema says: several <trk> and <trkseg> per file,
// points with no time, different time formats, vendor extensions,
// and — the case worth catching — files with no times at all.
//
// The parser makes no business decisions: it extracts, normalizes and counts. Who
// the seller is gets resolved in alias.go, and whether the day was good or bad, the
// metrics.
package gpx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

// Point is a GPS fix, already normalized.
type Point struct {
	Lat float64
	Lon float64
	Ele *float64
	// Ts is nil when the point carried no <time>.
	Ts *time.Time
	// Accuracy is the horizontal precision in metres, if the logger exports it.
	Accuracy *float64
	Seq      int
}

// Parsed is the result of reading a file.
type Parsed struct {
	Points []Point
	// NoTime counts the points that carried no <time>. If it equals len(Points),
	// there is no workday to measure.
	NoTime int
	// Hints are the texts the file's owner can be inferred from: track name,
	// author, the program that produced it.
	Hints    []string
	FirstFix *time.Time
	LastFix  *time.Time
	// Truncated: el fichero se acabó a media frase o traía XML roto, y lo que hay
	// aquí es lo que se pudo leer HASTA EL CORTE. El día no está completo.
	Truncated bool
	// Warning explica qué pasó, con las palabras del error de XML, para poder
	// enseñarlo en pantalla en vez de guardar un booleano mudo.
	Warning string
}

// --- XML deserialization structures ----------------------------------------

type xmlPunto struct {
	Lat        float64  `xml:"lat,attr"`
	Lon        float64  `xml:"lon,attr"`
	Ele        *float64 `xml:"ele"`
	Time       string   `xml:"time"`
	Hdop       *float64 `xml:"hdop"`
	Accuracy   *float64 `xml:"accuracy"`
	Extensions struct {
		Accuracy *float64 `xml:"accuracy"`
	} `xml:"extensions"`
}

type xmlGpx struct {
	Creator  string `xml:"creator,attr"`
	Metadata struct {
		Name   string `xml:"name"`
		Author struct {
			Name string `xml:"name"`
		} `xml:"author"`
	} `xml:"metadata"`
	Trk []struct {
		Name   string `xml:"name"`
		Trkseg []struct {
			Trkpt []xmlPunto `xml:"trkpt"`
		} `xml:"trkseg"`
	} `xml:"trk"`
	Rte []struct {
		Name  string     `xml:"name"`
		Rtept []xmlPunto `xml:"rtept"`
	} `xml:"rte"`
	Wpt []xmlPunto `xml:"wpt"`
}

// precision returns the declared precision in metres.
//
// Every logger puts it wherever it likes: <hdop> (dimensionless; approximated as
// ×5 m, the typical error of a fix with HDOP=1) or an accuracy extension already in
// metres. When absent the point is not penalised: missing data is not bad data.
func (p xmlPunto) precision() *float64 {
	if p.Accuracy != nil {
		return p.Accuracy
	}
	if p.Extensions.Accuracy != nil {
		return p.Extensions.Accuracy
	}
	if p.Hdop != nil {
		m := *p.Hdop * 5
		return &m
	}
	return nil
}

// timeFormats are the ones that show up in real .gpx files. The first is the
// standard's; the rest come from loggers that write the time their own way.
var timeFormats = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05.000Z",
	"2006-01-02 15:04:05",
}

func parseHora(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

// Parse reads a .gpx file.
//
// # Por qué se lee en flujo y no de una vez
//
// Antes esto era un `xml.Unmarshal` de todo el fichero, que es todo o nada: si el
// documento se acababa a media frase, el error se llevaba por delante las horas de
// recorrido que SÍ estaban escritas antes del corte, y ese día se perdía entero.
//
// Y pasa de verdad. Un .gpx de GPSLogger son doce megas que se van escribiendo
// mientras el vendedor anda; si el teléfono se queda sin batería, si lo matan a
// media escritura o si la subida a Drive se corta, el fichero llega con la mitad
// buena y el cierre sin escribir. Tirar cinco horas de ruta porque falta un
// `</gpx>` es tirar el dato por un detalle de sintaxis.
//
// Así que se leen los puntos DE UNO EN UNO y, en cuanto el documento deja de ser
// legible, se para y se devuelve lo que ya había, marcado como incompleto. Quien lo
// enseñe tiene que decirlo —ocho kilómetros de medio día no son los del día—, y para
// eso está `Truncated`.
//
// Solo se devuelve error cuando no se pudo rescatar NI UN punto: entonces sí, el
// fichero es basura y va a la bandeja para que alguien lo vuelva a subir.
func Parse(datos []byte) (*Parsed, error) {
	d := xml.NewDecoder(bytes.NewReader(datos))
	// Sin modo estricto: los exportadores reales escriben entidades sin declarar y
	// cierran mal las etiquetas, y con eso el decodificador se planta en el punto uno
	// aunque el resto del fichero esté impecable.
	d.Strict = false

	pistas := []string{}
	vistas := map[string]bool{}
	agregarPista := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !vistas[s] {
			vistas[s] = true
			pistas = append(pistas, s)
		}
	}

	puntos := []Point{}
	agregar := func(p xmlPunto) {
		// Coordenadas imposibles: exportador roto o fichero corrupto. Se descarta
		// el punto, no el fichero.
		if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) ||
			p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
			return
		}
		// The "null island": a fix with no signal that the logger writes as 0,0.
		if p.Lat == 0 && p.Lon == 0 {
			return
		}
		puntos = append(puntos, Point{
			Lat:      p.Lat,
			Lon:      p.Lon,
			Ele:      p.Ele,
			Ts:       parseHora(p.Time),
			Accuracy: p.precision(),
			Seq:      len(puntos),
		})
	}

	corte := ""

leyendo:
	for {
		tok, err := d.Token()
		if err != nil {
			// EOF limpio: el fichero se acabó donde tenía que acabarse.
			if !errors.Is(err, io.EOF) {
				corte = err.Error()
			}
			break
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch strings.ToLower(se.Name.Local) {
		case "gpx":
			for _, a := range se.Attr {
				if strings.EqualFold(a.Name.Local, "creator") {
					agregarPista(a.Value)
				}
			}

		case "trkpt", "rtept", "wpt":
			var p xmlPunto
			if err := d.DecodeElement(&p, &se); err != nil {
				// Aquí es donde muere un fichero cortado: el último punto está a
				// medio escribir. Se para y se conserva todo lo anterior — que es el
				// día entero menos el último segundo.
				if !errors.Is(err, io.EOF) {
					corte = err.Error()
				}
				break leyendo
			}
			agregar(p)

		case "name":
			// Los nombres —del track, de la ruta, del autor, del fichero— son las
			// pistas de quién subió esto. Se cogen todos sin mirar de dónde cuelgan:
			// cada exportador los pone en un sitio distinto y lo que importa es el
			// texto, no su rama.
			var t string
			if err := d.DecodeElement(&t, &se); err != nil {
				if !errors.Is(err, io.EOF) {
					corte = err.Error()
				}
				break leyendo
			}
			agregarPista(t)
		}
	}

	if len(puntos) == 0 {
		if corte != "" {
			return nil, fmt.Errorf("XML ilegible: %s", corte)
		}
		return nil, fmt.Errorf("el fichero no contiene puntos")
	}

	// Sort by time those that have one. Some loggers concatenate sessions out of
	// order, and an unsorted polyline draws a ball of yarn, not a route.
	sort.SliceStable(puntos, func(i, j int) bool {
		a, b := puntos[i], puntos[j]
		switch {
		case a.Ts != nil && b.Ts != nil:
			return a.Ts.Before(*b.Ts)
		case a.Ts != nil:
			return true
		case b.Ts != nil:
			return false
		default:
			return a.Seq < b.Seq
		}
	})
	for i := range puntos {
		puntos[i].Seq = i
	}

	res := &Parsed{Points: puntos, Hints: pistas}
	if corte != "" {
		res.Truncated = true
		res.Warning = fmt.Sprintf(
			"el fichero está cortado: se leyeron %d puntos hasta donde se pudo (%s)",
			len(puntos), corte)
	}
	for _, p := range puntos {
		if p.Ts == nil {
			res.NoTime++
			continue
		}
		if res.FirstFix == nil {
			res.FirstFix = p.Ts
		}
		res.LastFix = p.Ts
	}

	return res, nil
}
