// Package gpx lee los ficheros .gpx que los vendedores suben a las carpetas de
// Google Drive y decide de quién y de qué día es cada uno.
//
// El parser acepta lo que exportan los loggers reales, que es bastante más
// variado que lo que dice el esquema oficial: varios <trk> y <trkseg> por
// fichero, puntos sueltos como <wpt>, precisión en <hdop> o en extensiones
// propias, y —el caso que hay que cazar— ficheros sin ninguna hora.
//
// El parser no decide nada de negocio: extrae, normaliza y cuenta. Quién es el
// vendedor lo resuelve alias.go, y si el día fue bueno o malo, el paquete
// metricas.
package gpx

import (
	"encoding/xml"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Punto es un fix del GPS ya normalizado.
type Punto struct {
	Lat float64
	Lon float64
	Ele *float64
	// Ts es nil cuando el punto no traía <time>.
	Ts *time.Time
	// Accuracy es la precisión horizontal en metros, si el logger la exporta.
	Accuracy *float64
	Seq      int
}

// Parseado es el resultado de leer un fichero.
type Parseado struct {
	Puntos []Punto
	// SinHora cuenta los puntos que no traían <time>. Si iguala a len(Puntos),
	// no hay jornada que medir.
	SinHora int
	// Pistas son los textos de los que se puede deducir el dueño del fichero:
	// nombre de la pista, autor, programa que lo generó.
	Pistas    []string
	PrimerFix *time.Time
	UltimoFix *time.Time
}

// --- Estructuras de deserialización XML ------------------------------------

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

// precision devuelve la precisión declarada en metros.
//
// Cada logger la pone donde quiere: <hdop> (adimensional; se aproxima ×5 m, que
// es el error típico de un fix con HDOP=1) o una extensión accuracy ya en
// metros. Si no viene, el punto no se penaliza: ausencia de dato no es dato malo.
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

// formatosHora son los que aparecen en los .gpx reales. El primero es el del
// estándar; los demás salen de loggers que escriben la hora a su manera.
var formatosHora = []string{
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
	for _, f := range formatosHora {
		if t, err := time.Parse(f, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

// Parse lee un fichero .gpx.
//
// Devuelve error solo cuando el fichero entero es inservible (XML roto, sin
// raíz <gpx>, sin ningún punto). Un punto malo suelto no invalida el fichero:
// se descarta y el resto de la ruta se aprovecha.
func Parse(datos []byte) (*Parseado, error) {
	var doc xmlGpx
	if err := xml.Unmarshal(datos, &doc); err != nil {
		return nil, fmt.Errorf("XML ilegible: %w", err)
	}

	pistas := []string{}
	vistas := map[string]bool{}
	agregarPista := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !vistas[s] {
			vistas[s] = true
			pistas = append(pistas, s)
		}
	}
	agregarPista(doc.Creator)
	agregarPista(doc.Metadata.Name)
	agregarPista(doc.Metadata.Author.Name)

	puntos := []Punto{}
	agregar := func(p xmlPunto) {
		// Coordenadas imposibles: exportador roto o fichero corrupto. Se descarta
		// el punto, no el fichero.
		if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) ||
			p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
			return
		}
		// La "isla nula": un fix sin señal que el logger escribe como 0,0.
		if p.Lat == 0 && p.Lon == 0 {
			return
		}
		puntos = append(puntos, Punto{
			Lat:      p.Lat,
			Lon:      p.Lon,
			Ele:      p.Ele,
			Ts:       parseHora(p.Time),
			Accuracy: p.precision(),
			Seq:      len(puntos),
		})
	}

	for _, trk := range doc.Trk {
		agregarPista(trk.Name)
		for _, seg := range trk.Trkseg {
			for _, pt := range seg.Trkpt {
				agregar(pt)
			}
		}
	}
	for _, rte := range doc.Rte {
		agregarPista(rte.Name)
		for _, pt := range rte.Rtept {
			agregar(pt)
		}
	}
	for _, pt := range doc.Wpt {
		agregar(pt)
	}

	if len(puntos) == 0 {
		return nil, fmt.Errorf("el fichero no contiene puntos")
	}

	// Ordenar por hora los que la tengan. Hay loggers que concatenan sesiones
	// fuera de orden, y una polilínea desordenada dibuja un ovillo, no una ruta.
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

	res := &Parseado{Puntos: puntos, Pistas: pistas}
	for _, p := range puntos {
		if p.Ts == nil {
			res.SinHora++
			continue
		}
		if res.PrimerFix == nil {
			res.PrimerFix = p.Ts
		}
		res.UltimoFix = p.Ts
	}

	return res, nil
}
