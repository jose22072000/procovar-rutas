package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Volcado masivo de points, escrito a mano porque sqlc no cubre COPY con las
// columnas que aquí pueden ir nulas.
//
// Un día de un vendedor son ~2 500 fixes; con 8 sucursales, unos 141 000 al día.
// Con INSERT fila a fila el barrido nocturno no terminaría nunca: COPY mete un
// día entero en una sola operación.

// NewPoint es una fila de track_point lista para insertar.
type NewPoint struct {
	GpxFileID string
	SellerID  *string
	BranchID  *string
	Ts        *time.Time
	Lat       float64
	Lon       float64
	Ele       *float64
	Speed     *float64
	Accuracy  *float64
	Seq       int32
	Quality   PointQuality
}

// CopiadorPuntos es lo que hace falta de pgx para copiar: lo cumple tanto
// *pgxpool.Pool como pgx.Tx, de modo que el volcado puede ir dentro de la misma
// transacción que el registro del fichero.
type CopiadorPuntos interface {
	CopyFrom(ctx context.Context, tabla pgx.Identifier, columnas []string, filas pgx.CopyFromSource) (int64, error)
}

var columnasPunto = []string{
	"gpx_file_id", "trabajador_id", "sucursal_id", "ts",
	"lat", "lon", "ele", "speed", "accuracy", "seq", "quality",
}

// InsertPoints vuelca los points y devuelve cuántos entraron.
func InsertPoints(ctx context.Context, db CopiadorPuntos, points []NewPoint) (int64, error) {
	if len(points) == 0 {
		return 0, nil
	}
	filas := pgx.CopyFromSlice(len(points), func(i int) ([]any, error) {
		p := points[i]
		return []any{
			p.GpxFileID, p.SellerID, p.BranchID, p.Ts,
			p.Lat, p.Lon, p.Ele, p.Speed, p.Accuracy, p.Seq, p.Quality,
		}, nil
	})
	return db.CopyFrom(ctx, pgx.Identifier{"track_point"}, columnasPunto, filas)
}

// Nombres en inglés para las constantes de los enumerados.
//
// sqlc los bautiza a partir del nombre del tipo en Postgres (`estado_fichero` →
// `EstadoFicheroPROCESADO`), y el tipo de la base NO se toca. Así que el resto
// del código usa estos, y el generado se queda como está.
const (
	FileProcessed  = EstadoFicheroPROCESADO
	FileUnassigned = EstadoFicheroSINASIGNAR
	FileNoDate     = EstadoFicheroSINFECHA
	FileFailed     = EstadoFicheroERROR
	DayOk          = EstadoDiaOK
	DayNoFile      = EstadoDiaSINFICHERO
	DayNoDate      = EstadoDiaSINFECHA
	DayNoMovement  = EstadoDiaSINMOVIMIENTO
	DayLowMovement = EstadoDiaMOVIMIENTOESCASO
	DayNotWorking  = EstadoDiaNOLABORABLE
	PointOk        = CalidadPuntoOK
	PointJump      = CalidadPuntoSALTO
	PointDuplicate = CalidadPuntoDUPLICADO
	PointImprecise = CalidadPuntoIMPRECISO
	PointNoTime    = CalidadPuntoSINHORA
	SourceSeller   = TipoFuenteVENDEDOR
	SourceMixed    = TipoFuenteMIXTA
	SourceBranch   = TipoFuenteSUCURSAL
)
