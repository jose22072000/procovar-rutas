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
	"encoding/xml"
	"fmt"
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
// It only returns an error when the whole file is unusable (broken XML, no <gpx>
// root, not a single point). One bad point does not invalidate the file: it is
// dropped and the rest of the route is kept.
func Parse(datos []byte) (*Parsed, error) {
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

	puntos := []Point{}
	agregar := func(p xmlPunto) {
		// Coordenadas imposibles: exportador roto o fichero corrupto. Se descarta
		// the point, not the file.
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
