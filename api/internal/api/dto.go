package api

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
)

// Lo que sale por el API, en inglés y con forma propia.
//
// Antes los handlers serializaban directamente las structs que genera sqlc, y eso
// tenía dos problemas. Uno: las claves salían con el nombre del campo de Go
// (`TrabajadorID`, `KmNetos`), o sea el esquema de la base asomando por la API en
// dos idiomas a la vez. Y dos, más serio: cualquier `ALTER TABLE` cambiaba el JSON
// sin que nadie lo decidiera, y el front se rompía sin tocar una línea del front.
//
// Con estos tipos, el nombre de una columna es asunto de la base y el nombre de un
// campo del JSON es asunto del API. Se traduce aquí, en un solo sitio.

// SellerDay es una celda del calendario: un vendedor en un día.
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

func aSellerDay(f almacen.CalendarioRow) SellerDay {
	return SellerDay{
		SellerID: f.TrabajadorID,
		Seller:   f.Trabajador,
		BranchID: f.SucursalID,
		// Solo la fecha: la hora sobra en una cuadrícula por días y una marca
		// completa arrastraría la zona horaria del servidor al cliente.
		Date:       f.Fecha.Format(iso),
		Status:     string(f.Estado),
		NetKm:      f.KmNetos,
		Coverage:   f.Cobertura,
		FirstFix:   f.PrimerFix,
		LastFix:    f.UltimoFix,
		Flags:      f.Banderas,
		SpreadM:    f.RadioDispersion,
		PlaceLabel: f.LugarTexto,
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

func aSummaryRow(f almacen.ResumenIncidenciasRow) SummaryRow {
	return SummaryRow{
		SellerID:       f.TrabajadorID,
		Seller:         f.Trabajador,
		DaysNoFile:     f.SinFichero,
		DaysNoDate:     f.SinFecha,
		DaysNoMovement: f.SinMovimiento,
		DaysOk:         f.DiasOk,
		TotalKm:        f.KmTotal,
	}
}

// Seller es un vendedor en las listas y los desplegables.
type Seller struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	BranchID string `json:"branchId"`
	Active   bool   `json:"active"`
}

func aSeller(t almacen.Trabajador) Seller {
	return Seller{ID: t.ID, Name: t.Nombre, BranchID: t.SucursalID, Active: t.Activo}
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

func aDayDetail(d almacen.DiaDeTrabajadorRow, vendedor string) DayDetail {
	return DayDetail{
		ID:          d.ID,
		Seller:      vendedor,
		Date:        d.Fecha.Format(iso),
		Status:      string(d.Estado),
		NetKm:       d.KmNetos,
		Coverage:    d.Cobertura,
		MinMovement: d.MinMovimiento,
		MinStopped:  d.MinParado,
		Gaps:        d.Huecos,
		FirstFix:    d.PrimerFix,
		LastFix:     d.UltimoFix,
		SpreadM:     d.RadioDispersion,
		Flags:       d.Banderas,
		PlaceLabel:  d.LugarTexto,
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

func aTrackPoint(p almacen.PuntosDeDiaRow) TrackPoint {
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

func aStop(p almacen.Stop) Stop {
	return Stop{
		ID: p.ID, Start: p.Inicio, End: p.Fin, DurationMin: p.DuracionMin,
		Lat: p.Lat, Lon: p.Lon,
		ClientName: p.ClienteNombre, ClientDistM: p.DistanciaClienteM, Seq: p.Seq,
	}
}

// Los tres convierten una lista de golpe. Devuelven [] y no nil a propósito: `nil`
// se serializa como `null` y el front tendría que comprobarlo en cada sitio.
func aSellerDays(fs []almacen.CalendarioRow) []SellerDay {
	out := make([]SellerDay, 0, len(fs))
	for _, f := range fs {
		out = append(out, aSellerDay(f))
	}
	return out
}

// La semana de un vendedor sale de la tabla entera, no de la consulta del
// calendario, así que llega con otro tipo. Se convierte a la MISMA forma: para el
// front una celda es una celda, venga de donde venga.
func aSellerDaysDeTrackDay(ds []almacen.TrackDay, vendedor string) []SellerDay {
	out := make([]SellerDay, 0, len(ds))
	for _, d := range ds {
		out = append(out, SellerDay{
			SellerID:   d.TrabajadorID,
			Seller:     vendedor,
			BranchID:   d.SucursalID,
			Date:       d.Fecha.Format(iso),
			Status:     string(d.Estado),
			NetKm:      d.KmNetos,
			Coverage:   d.Cobertura,
			FirstFix:   d.PrimerFix,
			LastFix:    d.UltimoFix,
			Flags:      d.Banderas,
			SpreadM:    d.RadioDispersion,
			PlaceLabel: d.LugarTexto,
		})
	}
	return out
}

func aSummaryRows(fs []almacen.ResumenIncidenciasRow) []SummaryRow {
	out := make([]SummaryRow, 0, len(fs))
	for _, f := range fs {
		out = append(out, aSummaryRow(f))
	}
	return out
}

func aSellers(ts []almacen.Trabajador) []Seller {
	out := make([]Seller, 0, len(ts))
	for _, t := range ts {
		out = append(out, aSeller(t))
	}
	return out
}

func aTrackPoints(ps []almacen.PuntosDeDiaRow) []TrackPoint {
	out := make([]TrackPoint, 0, len(ps))
	for _, p := range ps {
		out = append(out, aTrackPoint(p))
	}
	return out
}

func aStops(ps []almacen.Stop) []Stop {
	out := make([]Stop, 0, len(ps))
	for _, p := range ps {
		out = append(out, aStop(p))
	}
	return out
}
