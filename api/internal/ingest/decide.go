// Package ingest pulls .gpx files from Drive, stores them and recomputes each
// seller's day.
//
// This file holds the part that DECIDES — whose file this is, which day it
// belongs to, whether it is usable — kept apart from the part that writes to the
// database. The decision is a pure function and is tested end to end without
// Postgres or Google credentials; the rest is just inserting rows.
package ingest

import (
	"time"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
)

// Status of a file after inspecting it. Mirrors the estado_fichero enum in the
// database.
type Status string

const (
	StatusProcessed  Status = "PROCESADO"
	StatusUnassigned Status = "SIN_ASIGNAR"
	StatusNoDate     Status = "SIN_FECHA"
	StatusFailed     Status = "ERROR"
)

// DateSource says where the day's date came from. A day dated from the file name
// is not worth the same as one dated from its own points, and the panel shows
// which is which.
type DateSource string

const (
	DateFromPoints DateSource = "PUNTOS"
	DateFromName   DateSource = "NOMBRE"
	DateFromDrive  DateSource = "DRIVE"
	DateNone       DateSource = "NINGUNO"
)

// Verdict is everything known about a file before touching the database.
type Verdict struct {
	Status   Status
	Error    string
	SellerID string
	Via      gpx.Via
	// AliasHint is the text shown to the admin in the inbox.
	AliasHint string
	// Date is the local day, "YYYY-MM-DD". Empty when it could not be dated.
	Date       string
	DateSource DateSource
	Parsed     *gpx.Parsed
}

// Env is what has to be known in order to judge a file.
type Env struct {
	SourceType     gpx.SourceType
	SourceSellerID string
	// SourceName is the name of the registered folder.
	SourceName string
	// Normalized alias -> seller id.
	Alias map[string]string
	// Branch time zone, to turn a UTC instant into a local day.
	Zone *time.Location
}

// Examine decides what to do with a freshly downloaded file.
//
// It never returns an error: a file that cannot be read, or whose owner is
// unknown, is NOT discarded — it is recorded with its status and its hint so it
// shows up in the inbox. No file is ever lost in silence.
func Examine(f drive.File, datos []byte, ent Env) Verdict {
	parseado, err := gpx.Parse(datos)
	if err != nil {
		return Verdict{
			Status:     StatusFailed,
			Error:      err.Error(),
			AliasHint:  f.Name,
			DateSource: DateNone,
		}
	}

	res := gpx.ResolveSeller(gpx.Context{
		SourceType:     ent.SourceType,
		SourceSellerID: ent.SourceSellerID,
		SourceName:     ent.SourceName,
		FolderPath:     f.FolderPath,
		FileName:       f.Name,
		GpxHints:       parseado.Hints,
		Alias:          ent.Alias,
	})

	v := Verdict{
		SellerID:  res.SellerID,
		Via:       res.Via,
		AliasHint: res.Hint,
		Parsed:    parseado,
	}

	// Un fichero cortado SÍ se da por bueno —sus puntos son buenos— pero se queda con
	// el aviso escrito. El estado dice que se pudo usar; el aviso, que lo que entró
	// no es el día entero. Perder eso sería enseñar ocho kilómetros de medio día como
	// si fueran los del día.
	if parseado.Truncated {
		v.Error = parseado.Warning
	}
	if v.AliasHint == "" {
		v.AliasHint = f.Name
	}

	// The date, in order of reliability.
	zona := ent.Zone
	if zona == nil {
		zona = time.UTC
	}
	switch {
	case parseado.FirstFix != nil:
		v.Date = parseado.FirstFix.In(zona).Format("2006-01-02")
		v.DateSource = DateFromPoints
	case gpx.DateFromName(f.Name) != "":
		v.Date = gpx.DateFromName(f.Name)
		v.DateSource = DateFromName
	case !f.Created.IsZero():
		// The weakest one: the Drive upload date. It can be days later than the
		// route itself, so the day is flagged as inferred and the panel paints it
		// differently instead of passing off a guess as fact.
		v.Date = f.Created.In(zona).Format("2006-01-02")
		v.DateSource = DateFromDrive
	default:
		v.DateSource = DateNone
	}

	switch {
	case res.SellerID == "":
		v.Status = StatusUnassigned
	case v.Date == "":
		v.Status = StatusNoDate
	default:
		v.Status = StatusProcessed
	}

	return v
}
