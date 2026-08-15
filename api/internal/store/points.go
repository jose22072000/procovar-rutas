package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Bulk load of points, written by hand because sqlc does not cover COPY with the
// nullable columns used here.
//
// One seller's day is ~2,500 fixes; with 8 branches, around 141,000 a day. With
// row-by-row INSERTs the nightly sweep would never finish: COPY puts a whole day
// in with a single operation.

// NewPoint is a track_point row ready to be inserted.
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

// PointCopier is what pgx needs to copy: both *pgxpool.Pool and pgx.Tx satisfy
// it, so the bulk load can run inside the same transaction as the file record.
type PointCopier interface {
	CopyFrom(ctx context.Context, tabla pgx.Identifier, columnas []string, filas pgx.CopyFromSource) (int64, error)
}

var columnasPunto = []string{
	"gpx_file_id", "trabajador_id", "sucursal_id", "ts",
	"lat", "lon", "ele", "speed", "accuracy", "seq", "quality",
}

// InsertPoints loads the points and returns how many went in.
func InsertPoints(ctx context.Context, db PointCopier, points []NewPoint) (int64, error) {
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

// English names for the enum constants.
//
// sqlc names them after the Postgres type (`estado_fichero` →
// `EstadoFicheroPROCESADO`), and the database type is NOT being touched. So the
// rest of the code uses these and the generated file stays as it is.
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
