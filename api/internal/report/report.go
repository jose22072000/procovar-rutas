// Package report builds the per-seller document.
//
// What was asked for: Monday to Friday, and for each day the table with EVERY
// movement between 9:00 and 16:00, in rows alternating travel and stop.
// parada.
//
// Building it is a pure function over what is already in the database — it does
// not recompute kilometres or re-detect stops. If the report did its own maths it
// would end up showing different numbers from the panel for the same day, which
// is the fastest way to make nobody trust either of them.
package report

import (
	"fmt"
	"math"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/metrics"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

type Header struct {
	Seller   string `json:"seller"`
	SellerID string `json:"sellerId"`
	From     string `json:"from"`
	To       string `json:"to"`
	Workday  string `json:"workday"`
	Timezone string `json:"timezone"`
}

// Movement is one row of the day's table.
type Movement struct {
	// Type: "desplazamiento" o "parada".
	Type        string  `json:"type"`
	StartTime   string  `json:"startTime"`
	EndTime     string  `json:"endTime"`
	DurationMin int     `json:"durationMin"`
	DistanceKm  float64 `json:"distanceKm"`
	AvgSpeed    float64 `json:"avgSpeed"`
	MaxSpeed    float64 `json:"maxSpeed"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	// Place is the nearest client, when crossed with the client geolocation data.
	Place string `json:"place,omitempty"`
}

type Day struct {
	Date   string `json:"date"`
	Status string `json:"status"`
	// Reason explains in words why the day is not a good one. A bad day gets its
	// section just like a good one: it is the one worth showing.
	Reason      string     `json:"reason,omitempty"`
	FirstFix    string     `json:"firstFix,omitempty"`
	LastFix     string     `json:"lastFix,omitempty"`
	NetKm       float64    `json:"netKm"`
	Coverage    float64    `json:"coverage"`
	MinStopped  int        `json:"minStopped"`
	MinMovement int        `json:"minMovement"`
	Flags       []string   `json:"flags"`
	Movements   []Movement `json:"movements"`
	// Track are the points used to draw the section's map.
	Track []Point `json:"track"`
	Place string  `json:"place,omitempty"`
}

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Ts  string  `json:"ts,omitempty"`
}

type Summary struct {
	DaysOk         int     `json:"daysOk"`
	DaysNoFile     int     `json:"daysNoFile"`
	DaysNoDate     int     `json:"daysNoDate"`
	DaysNoMovement int     `json:"daysNoMovement"`
	TotalKm        float64 `json:"totalKm"`
	Stops          int     `json:"stops"`
	AvgCoverage    float64 `json:"avgCoverage"`
}

type Document struct {
	Header  Header  `json:"header"`
	Summary Summary `json:"summary"`
	Days    []Day   `json:"days"`
}

var motivos = map[string]string{
	"SIN_FICHERO":       "No se subió ningún fichero de recorrido este día.",
	"SIN_FECHA":         "El fichero llegó sin horas, así que no se pudo medir la jornada.",
	"SIN_MOVIMIENTO":    "Los puntos no se alejaron del mismo lugar en toda la jornada.",
	"MOVIMIENTO_ESCASO": "Se movió, pero muy por debajo de una ruta normal.",
	"NO_LABORABLE":      "Día no laborable.",
}

// EmptyDay is a working day with not even a row in the database.
func EmptyDay(fecha string) Day {
	return Day{
		Date:      fecha,
		Status:    "SIN_FICHERO",
		Reason:    motivos["SIN_FICHERO"],
		Flags:     []string{},
		Movements: []Movement{},
		Track:     []Point{},
	}
}

// BuildDay assembles a day's section: the table of movements alternating travel
// and stops, plus the map points.
func BuildDay(
	d store.TrackDay,
	puntos []store.DayPointsRow,
	paradas []store.Stop,
	zona *time.Location,
) Day {
	dia := Day{
		Date:        d.Date.Format("2006-01-02"),
		Status:      string(d.Status),
		Reason:      motivos[string(d.Status)],
		NetKm:       d.NetKm,
		Coverage:    d.Coverage,
		MinStopped:  int(d.MinStopped),
		MinMovement: int(d.MinMovement),
		Flags:       d.Flags,
		Movements:   []Movement{},
		Track:       make([]Point, 0, len(puntos)),
	}
	if d.PlaceLabel != nil {
		dia.Place = *d.PlaceLabel
	}
	if d.FirstFix != nil {
		dia.FirstFix = d.FirstFix.In(zona).Format("15:04")
	}
	if d.LastFix != nil {
		dia.LastFix = d.LastFix.In(zona).Format("15:04")
	}

	for _, p := range puntos {
		pt := Point{Lat: p.Lat, Lon: p.Lon}
		if p.Ts != nil {
			pt.Ts = p.Ts.In(zona).Format("15:04:05")
		}
		dia.Track = append(dia.Track, pt)
	}

	dia.Movements = movements(puntos, paradas, zona)
	return dia
}

// movements intercala paradas y desplazamientos en orden cronológico.
//
// Travel legs are "whatever is left between two stops": they are computed by
// difference rather than on their own, so the table's total always matches the
// day's.
func movements(
	puntos []store.DayPointsRow,
	paradas []store.Stop,
	zona *time.Location,
) []Movement {
	out := []Movement{}
	if len(puntos) == 0 {
		return out
	}

	corte := 0
	for _, p := range paradas {
		// The leg before this stop.
		ini := corte
		for corte < len(puntos) && puntos[corte].Ts != nil && puntos[corte].Ts.Before(p.Start) {
			corte++
		}
		if m, ok := leg(puntos[ini:corte], zona); ok {
			out = append(out, m)
		}

		out = append(out, Movement{
			Type:        "parada",
			StartTime:   p.Start.In(zona).Format("15:04"),
			EndTime:     p.End.In(zona).Format("15:04"),
			DurationMin: int(p.DurationMin),
			Lat:         p.Lat,
			Lon:         p.Lon,
			Place:       stopPlace(p),
		})

		// Skip the points that fall inside the stop.
		for corte < len(puntos) && puntos[corte].Ts != nil && !puntos[corte].Ts.After(p.End) {
			corte++
		}
	}

	// Whatever is left after the last stop.
	if m, ok := leg(puntos[corte:], zona); ok {
		out = append(out, m)
	}

	return out
}

func leg(puntos []store.DayPointsRow, zona *time.Location) (Movement, bool) {
	if len(puntos) < 2 || puntos[0].Ts == nil || puntos[len(puntos)-1].Ts == nil {
		return Movement{}, false
	}

	var metros, velMax float64
	for i := 1; i < len(puntos); i++ {
		metros += metrics.DistanceM(
			metrics.Coord{Lat: puntos[i-1].Lat, Lon: puntos[i-1].Lon},
			metrics.Coord{Lat: puntos[i].Lat, Lon: puntos[i].Lon})
		if puntos[i].Speed != nil && *puntos[i].Speed > velMax {
			velMax = *puntos[i].Speed
		}
	}

	inicio := *puntos[0].Ts
	fin := *puntos[len(puntos)-1].Ts
	minutos := int(fin.Sub(inicio).Minutes())

	km := round(metros/1000, 2)
	media := 0.0
	if minutos > 0 {
		media = round(km/(float64(minutos)/60), 1)
	}

	return Movement{
		Type:        "desplazamiento",
		StartTime:   inicio.In(zona).Format("15:04"),
		EndTime:     fin.In(zona).Format("15:04"),
		DurationMin: minutos,
		DistanceKm:  km,
		AvgSpeed:    media,
		MaxSpeed:    round(velMax, 1),
		Lat:         puntos[len(puntos)-1].Lat,
		Lon:         puntos[len(puntos)-1].Lon,
	}, true
}

func stopPlace(p store.Stop) string {
	if p.ClientName == nil {
		return ""
	}
	if p.ClientDistM != nil {
		return fmt.Sprintf("a %.0f m de %s", *p.ClientDistM, *p.ClientName)
	}
	return *p.ClientName
}

// Build closes the document with its summary.
func Build(cab Header, dias []Day) Document {
	doc := Document{Header: cab, Days: dias}

	conDatos := 0
	for _, d := range dias {
		switch d.Status {
		case "OK":
			doc.Summary.DaysOk++
		case "SIN_FICHERO":
			doc.Summary.DaysNoFile++
		case "SIN_FECHA":
			doc.Summary.DaysNoDate++
		case "SIN_MOVIMIENTO":
			doc.Summary.DaysNoMovement++
		}
		doc.Summary.TotalKm += d.NetKm
		for _, m := range d.Movements {
			if m.Type == "parada" {
				doc.Summary.Stops++
			}
		}
		if d.Coverage > 0 {
			doc.Summary.AvgCoverage += d.Coverage
			conDatos++
		}
	}

	doc.Summary.TotalKm = round(doc.Summary.TotalKm, 2)
	if conDatos > 0 {
		doc.Summary.AvgCoverage = round(doc.Summary.AvgCoverage/float64(conDatos), 1)
	}

	return doc
}

func round(v float64, decimales int) float64 {
	f := math.Pow(10, float64(decimales))
	return math.Round(v*f) / f
}
