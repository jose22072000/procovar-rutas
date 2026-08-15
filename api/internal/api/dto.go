package api

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Lo que sale por el API, en inglés y con forma propia.
//
// Antes los handlers serializaban directamente las structs que genera sqlc, y eso
// tenía dos problemas. Uno: las claves salían con el nombre del campo de Go
// (`SellerID`, `KmNetos`), o sea el esquema de la base asomando por la API en
// dos idiomas a la vez. Y dos, más serio: cualquier `ALTER TABLE` cambiaba el JSON
// sin que nadie lo decidiera, y el front se rompía sin tocar una línea del front.
//
// Con estos tipos, el nombre de una columna es asunto de la base y el nombre de un
// campo del JSON es asunto del API. Se traduce aquí, en un solo sitio.

// SellerDay es una celda del calendar: un vendedor en un día.
type SellerDay struct {
	SellerID   string     `json:"sellerId"`
	Seller     string     `json:"seller"`
	BranchID   string     `json:"branchId"`
	Date       string     `json:"date"`
	Status     string     `json:"status"`
	NetKm      float64    `json:"netKm"`
	Coverage   float64    `json:"coverage"`
	FirstFix   *time.Time `json:"firstFix"`
	LastFix    *time.Time `json:"lastFix"`
	Flags      []string   `json:"flags"`
	SpreadM    *float64   `json:"spreadM"`
	PlaceLabel *string    `json:"placeLabel"`
}

func aSellerDay(f store.CalendarRow) SellerDay {
	return SellerDay{
		SellerID: f.SellerID,
		Seller:   f.Seller,
		BranchID: f.BranchID,
		// Solo la fecha: la hora sobra en una cuadrícula por días y una marca
		// completa arrastraría la zona horaria del servidor al cliente.
		Date:       f.Date.Format(iso),
		Status:     string(f.Status),
		NetKm:      f.NetKm,
		Coverage:   f.Coverage,
		FirstFix:   f.FirstFix,
		LastFix:    f.LastFix,
		Flags:      f.Flags,
		SpreadM:    f.SpreadM,
		PlaceLabel: f.PlaceLabel,
	}
}

// SummaryRow es el recuento de incidencias de un vendedor en el rango pedido.
type SummaryRow struct {
	SellerID       string  `json:"sellerId"`
	Seller         string  `json:"seller"`
	DaysNoFile     int64   `json:"daysNoFile"`
	DaysNoDate     int64   `json:"daysNoDate"`
	DaysNoMovement int64   `json:"daysNoMovement"`
	DaysOk         int64   `json:"daysOk"`
	TotalKm        float64 `json:"totalKm"`
}

func aSummaryRow(f store.IncidentSummaryRow) SummaryRow {
	return SummaryRow{
		SellerID:       f.SellerID,
		Seller:         f.Seller,
		DaysNoFile:     f.DaysNoFile,
		DaysNoDate:     f.DaysNoDate,
		DaysNoMovement: f.DaysNoMovement,
		DaysOk:         f.DaysOk,
		TotalKm:        f.TotalKm,
	}
}

// Seller es un vendedor en las listas y los desplegables.
type Seller struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BranchID string `json:"branchId"`
	Active   bool   `json:"active"`
}

func aSeller(t store.Seller) Seller {
	return Seller{ID: t.ID, Name: t.Name, BranchID: t.BranchID, Active: t.Active}
}

// DayDetail es la ficha del día que abre el visor del mapa.
type DayDetail struct {
	ID          string     `json:"id"`
	Seller      string     `json:"seller"`
	Date        string     `json:"date"`
	Status      string     `json:"status"`
	NetKm       float64    `json:"netKm"`
	Coverage    float64    `json:"coverage"`
	MinMovement int32      `json:"minMovement"`
	MinStopped  int32      `json:"minStopped"`
	Gaps        int32      `json:"gaps"`
	FirstFix    *time.Time `json:"firstFix"`
	LastFix     *time.Time `json:"lastFix"`
	SpreadM     *float64   `json:"spreadM"`
	Flags       []string   `json:"flags"`
	PlaceLabel  *string    `json:"placeLabel"`
}

func aDayDetail(d store.SellerDayRow, vendedor string) DayDetail {
	return DayDetail{
		ID:          d.ID,
		Seller:      vendedor,
		Date:        d.Date.Format(iso),
		Status:      string(d.Status),
		NetKm:       d.NetKm,
		Coverage:    d.Coverage,
		MinMovement: d.MinMovement,
		MinStopped:  d.MinStopped,
		Gaps:        d.Gaps,
		FirstFix:    d.FirstFix,
		LastFix:     d.LastFix,
		SpreadM:     d.SpreadM,
		Flags:       d.Flags,
		PlaceLabel:  d.PlaceLabel,
	}
}

// TrackPoint es un punto del recorrido dibujado en el mapa.
type TrackPoint struct {
	Ts      *time.Time `json:"ts"`
	Lat     float64    `json:"lat"`
	Lon     float64    `json:"lon"`
	Speed   *float64   `json:"speed"`
	Quality string     `json:"quality"`
	Seq     int32      `json:"seq"`
}

func aTrackPoint(p store.DayPointsRow) TrackPoint {
	return TrackPoint{
		Ts: p.Ts, Lat: p.Lat, Lon: p.Lon,
		Speed: p.Speed, Quality: string(p.Quality), Seq: p.Seq,
	}
}

// Stop es una parada: donde el vendedor estuvo quieto el rato suficiente.
type Stop struct {
	ID          string    `json:"id"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	DurationMin int32     `json:"durationMin"`
	Lat         float64   `json:"lat"`
	Lon         float64   `json:"lon"`
	ClientName  *string   `json:"clientName"`
	ClientDistM *float64  `json:"clientDistM"`
	Seq         int32     `json:"seq"`
}

func aStop(p store.Stop) Stop {
	return Stop{
		ID: p.ID, Start: p.Start, End: p.End, DurationMin: p.DurationMin,
		Lat: p.Lat, Lon: p.Lon,
		ClientName: p.ClientName, ClientDistM: p.ClientDistM, Seq: p.Seq,
	}
}

// Los tres convierten una lista de golpe. Devuelven [] y no nil a propósito: `nil`
// se serializa como `null` y el front tendría que comprobarlo en cada sitio.
func aSellerDays(fs []store.CalendarRow) []SellerDay {
	out := make([]SellerDay, 0, len(fs))
	for _, f := range fs {
		out = append(out, aSellerDay(f))
	}
	return out
}

// La week de un vendedor sale de la tabla entera, no de la consulta del
// calendar, así que llega con otro tipo. Se convierte a la MISMA forma: para el
// front una celda es una celda, venga de donde venga.
func aSellerDaysFromTrackDay(ds []store.TrackDay, vendedor string) []SellerDay {
	out := make([]SellerDay, 0, len(ds))
	for _, d := range ds {
		out = append(out, SellerDay{
			SellerID:   d.SellerID,
			Seller:     vendedor,
			BranchID:   d.BranchID,
			Date:       d.Date.Format(iso),
			Status:     string(d.Status),
			NetKm:      d.NetKm,
			Coverage:   d.Coverage,
			FirstFix:   d.FirstFix,
			LastFix:    d.LastFix,
			Flags:      d.Flags,
			SpreadM:    d.SpreadM,
			PlaceLabel: d.PlaceLabel,
		})
	}
	return out
}

func aSummaryRows(fs []store.IncidentSummaryRow) []SummaryRow {
	out := make([]SummaryRow, 0, len(fs))
	for _, f := range fs {
		out = append(out, aSummaryRow(f))
	}
	return out
}

func aSellers(ts []store.Seller) []Seller {
	out := make([]Seller, 0, len(ts))
	for _, t := range ts {
		out = append(out, aSeller(t))
	}
	return out
}

func aTrackPoints(ps []store.DayPointsRow) []TrackPoint {
	out := make([]TrackPoint, 0, len(ps))
	for _, p := range ps {
		out = append(out, aTrackPoint(p))
	}
	return out
}

func aStops(ps []store.Stop) []Stop {
	out := make([]Stop, 0, len(ps))
	for _, p := range ps {
		out = append(out, aStop(p))
	}
	return out
}
