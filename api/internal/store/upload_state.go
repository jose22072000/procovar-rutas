package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Cuándo subió cada vendedor por última vez, escrito a mano.
//
// Es lo que convierte un "sin fichero" en una frase que se puede accionar: uno que
// no subió AYER es un despiste, uno que lleva veinte días es un GPS que hay que ir
// a mirar. Vivía en la pantalla de Administración, donde nadie entraba.
//
// A mano y no con sqlc porque aquí todo lo interesante es ANULABLE —quien nunca
// subió nada no tiene última subida— y sqlc, con las subconsultas y el LEFT JOIN
// que esto necesita, devolvía `interface{}` en la mitad de las columnas. Un
// `interface{}` en el sitio de una fecha es un error de escaneo esperando a que
// alguien abra la semana equivocada.

// UploadState is what is known about a seller beyond the range being looked at.
type UploadState struct {
	SellerID string
	// LastUpload is nil when nothing has ever come in through that seller.
	LastUpload *time.Time
	// StuckFiles: files of theirs that arrived and could not be used.
	StuckFiles int
	TotalFiles int
	// Linked: whether they are paired with a PEDIDO vendor. Without that, their
	// orders cross with no route and the panel has to say so.
	Linked bool
}

const consultaUploadState = `
WITH ultima AS (
    -- La última subida sale del REGISTRO DE FICHEROS, no de track_day.
    --
    -- Antes se miraba track_day.gpx_file_id IS NOT NULL, y esa columna no la rellena
    -- nadie: RecomputeDay guarda el día sin ella porque recalcula desde los puntos,
    -- que pueden venir de varios ficheros. Resultado: el panel decía que los treinta
    -- y seis vendedores no habían subido NUNCA, con dos mil días de recorrido
    -- cargados. Un aviso que sale para todos no avisa de nada.
    SELECT f.trabajador_id, max(f.fecha) AS fecha
    FROM gpx_file f
    WHERE f.estado = 'PROCESADO' AND f.fecha IS NOT NULL
    GROUP BY f.trabajador_id
),
ficheros AS (
    SELECT f.trabajador_id,
           count(*) FILTER (WHERE f.estado <> 'PROCESADO') AS atascados,
           count(*) AS total
    FROM gpx_file f
    WHERE f.trabajador_id IS NOT NULL
    GROUP BY f.trabajador_id
)
SELECT
    t.id,
    u.fecha,
    coalesce(fi.atascados, 0),
    coalesce(fi.total, 0),
    EXISTS (SELECT 1 FROM vendedor_pedido v WHERE v.trabajador_id = t.id)
FROM trabajador t
LEFT JOIN ultima u   ON u.trabajador_id = t.id
LEFT JOIN ficheros fi ON fi.trabajador_id = t.id
WHERE t.activo AND t.es_vendedor
  AND ($1::text = '' OR t.sucursal_id = $1)
  AND (cardinality($2::text[]) = 0 OR t.id = ANY ($2))
  AND ($3::text = '' OR t.id <> $3)`

// UploadStates applies the same scope block as every panel query: the three
// parameters always come out of scope.Compute.
func UploadStates(ctx context.Context, pool *pgxpool.Pool, branchID string, sellers []string, exclude string) ([]UploadState, error) {
	if sellers == nil {
		sellers = []string{}
	}
	filas, err := pool.Query(ctx, consultaUploadState, branchID, sellers, exclude)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	out := []UploadState{}
	for filas.Next() {
		var e UploadState
		if err := filas.Scan(&e.SellerID, &e.LastUpload, &e.StuckFiles, &e.TotalFiles, &e.Linked); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, filas.Err()
}
