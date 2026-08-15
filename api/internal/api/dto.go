package api

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

// What the API sends out, in English and with a shape of its own.
//
// The handlers used to serialize sqlc's structs directly, and that had two
// problems. One: the keys came out with the Go field names (`SellerID`,
// `KmNetos`) — the database schema showing through the API, in two languages at
// once. And two, more serious: any `ALTER TABLE` changed the JSON without anyone
// deciding to, and the front end broke without a line of the front end changing.
//
// With these types, a column's name is the database's business and a JSON field's
// name is the API's. The translation happens here, in one place.

// SellerDay is one calendar cell: a seller on a day.
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
		// Date only: the time is noise in a per-day grid, and a full timestamp would
		// drag the server's time zone out to the client.
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

// SummaryRow is a seller's incident count over the requested range.
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

// Seller is a seller as shown in lists and dropdowns.
type Seller struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BranchID string `json:"branchId"`
	Active   bool   `json:"active"`
}

func aSeller(t store.Seller) Seller {
	return Seller{ID: t.ID, Name: t.Name, BranchID: t.BranchID, Active: t.Active}
}

// DayDetail is the day record the map viewer opens.
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

// TrackPoint is one point of the route drawn on the map.
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

// Stop is a stop: where the seller stayed put long enough.
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

// These convert a whole list at once. They return [] and not nil on purpose: `nil`
// serializes as `null` and the front end would have to check for it everywhere.
func aSellerDays(fs []store.CalendarRow) []SellerDay {
	out := make([]SellerDay, 0, len(fs))
	for _, f := range fs {
		out = append(out, aSellerDay(f))
	}
	return out
}

// A seller's week comes from the whole table, not from the calendar query, so it
// arrives as a different type. It is converted to the SAME shape: to the front end
// a cell is a cell, wherever it came from.
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
