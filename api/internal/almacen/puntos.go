package almacen

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Volcado masivo de puntos, escrito a mano porque sqlc no cubre COPY con las
// columnas que aquí pueden ir nulas.
//
// Un día de un vendedor son ~2 500 fixes; con 8 sucursales, unos 141 000 al día.
// Con INSERT fila a fila el barrido nocturno no terminaría nunca: COPY mete un
// día entero en una sola operación.

// PuntoNuevo es una fila de track_point lista para insertar.
type PuntoNuevo struct {
	GpxFileID    string
	TrabajadorID *string
	SucursalID   *string
	Ts           *time.Time
	Lat          float64
	Lon          float64
	Ele          *float64
	Speed        *float64
	Accuracy     *float64
	Seq          int32
	Quality      CalidadPunto
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

// InsertarPuntos vuelca los puntos y devuelve cuántos entraron.
func InsertarPuntos(ctx context.Context, db CopiadorPuntos, puntos []PuntoNuevo) (int64, error) {
	if len(puntos) == 0 {
		return 0, nil
	}
	filas := pgx.CopyFromSlice(len(puntos), func(i int) ([]any, error) {
		p := puntos[i]
		return []any{
			p.GpxFileID, p.TrabajadorID, p.SucursalID, p.Ts,
			p.Lat, p.Lon, p.Ele, p.Speed, p.Accuracy, p.Seq, p.Quality,
		}, nil
	})
	return db.CopyFrom(ctx, pgx.Identifier{"track_point"}, columnasPunto, filas)
}
