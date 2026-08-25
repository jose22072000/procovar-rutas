-- Panel queries. Every one of them carries THE SAME scope block:
--
--   (@branch_id = ''  OR sucursal_id = @branch_id)
--   (cardinality(@sellers) = 0 OR trabajador_id = ANY(@sellers))
--   (@exclude = ''      OR trabajador_id <> @exclude)
--
-- which is the literal translation of internal/scope.Filter. It is repeated in the
-- SQL because sqlc leaves no other way, but NO query builds it on its own: the
-- three parameters always come out of scope.Compute. The day someone writes a new
-- query without them, review should reject it.
--
-- The columns stay in Spanish because the database is not being renamed; the
-- translation to English happens when sqlc generates the Go (see sqlc.yaml).

-- The compliance calendar: the grid of sellers × working days.
-- name: Calendar :many
SELECT
    d.trabajador_id,
    t.nombre AS seller,
    d.sucursal_id,
    coalesce(s.nombre, '') AS branch,
    d.fecha,
    d.estado,
    d.km_netos,
    d.cobertura,
    d.primer_fix,
    d.ultimo_fix,
    d.banderas,
    d.radio_dispersion,
    d.lugar_texto
FROM track_day d
JOIN trabajador t ON t.id = d.trabajador_id
LEFT JOIN sucursal s ON s.id = d.sucursal_id
WHERE d.fecha BETWEEN @from_date::date AND @to_date::date
  AND (@branch_id::text = '' OR d.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR d.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR d.trabajador_id <> @exclude)
ORDER BY t.nombre, d.fecha;

-- name: IncidentSummary :many
SELECT
    d.trabajador_id,
    t.nombre AS seller,
    count(*) FILTER (WHERE d.estado = 'SIN_FICHERO')    AS days_no_file,
    count(*) FILTER (WHERE d.estado = 'SIN_FECHA')      AS days_no_date,
    count(*) FILTER (WHERE d.estado = 'SIN_MOVIMIENTO') AS days_no_movement,
    count(*) FILTER (WHERE d.estado = 'OK')             AS days_ok,
    coalesce(sum(d.km_netos), 0)::float8                AS total_km
FROM track_day d
JOIN trabajador t ON t.id = d.trabajador_id
WHERE d.fecha BETWEEN @from_date::date AND @to_date::date
  AND d.estado <> 'NO_LABORABLE'
  AND (@branch_id::text = '' OR d.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR d.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR d.trabajador_id <> @exclude)
GROUP BY d.trabajador_id, t.nombre
-- Postgres does NOT accept an output alias inside an ORDER BY expression (alone
-- yes, summed no), so the counts are repeated here. Writing it with the aliases
-- compiles in sqlc and blows up in the database: "column ... does not exist".
ORDER BY count(*) FILTER (WHERE d.estado = 'SIN_FICHERO')
       + count(*) FILTER (WHERE d.estado = 'SIN_FECHA')
       + count(*) FILTER (WHERE d.estado = 'SIN_MOVIMIENTO') DESC,
         t.nombre;

-- name: SellerDay :one
SELECT d.*, t.nombre AS seller, coalesce(s.nombre, '') AS branch
FROM track_day d
JOIN trabajador t ON t.id = d.trabajador_id
LEFT JOIN sucursal s ON s.id = d.sucursal_id
WHERE d.trabajador_id = @seller_id AND d.fecha = @date::date
  AND (@branch_id::text = '' OR d.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR d.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR d.trabajador_id <> @exclude);

-- The points the viewer draws. Working hours only by default; the "full day"
-- switch sends @workday_start = '00:00' and @workday_end = '23:59'.
-- name: DayPoints :many
SELECT p.ts, p.lat, p.lon, p.ele, p.speed, p.quality, p.seq
FROM track_point p
JOIN track_day d ON d.trabajador_id = p.trabajador_id
                AND d.fecha = (p.ts AT TIME ZONE @zone::text)::date
WHERE d.id = @track_day_id
  AND p.ts IS NOT NULL
  AND to_char(p.ts AT TIME ZONE @zone::text, 'HH24:MI') BETWEEN @workday_start::text AND @workday_end::text
ORDER BY p.ts;

-- A file with no times does not match the previous query (it has no ts), but its
-- route can still be drawn: that is what these points are for, in sequence order.
--
-- name: FilePoints :many
SELECT p.ts, p.lat, p.lon, p.ele, p.speed, p.quality, p.seq
FROM track_point p
WHERE p.gpx_file_id = $1
ORDER BY p.seq;

-- name: DayStops :many
SELECT * FROM stop WHERE track_day_id = $1 ORDER BY seq;

-- name: SellerWeek :many
SELECT d.*
FROM track_day d
WHERE d.trabajador_id = @seller_id
  AND d.fecha BETWEEN @from_date::date AND @to_date::date
  AND (@branch_id::text = '' OR d.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR d.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR d.trabajador_id <> @exclude)
ORDER BY d.fecha;

-- name: SellersInScope :many
SELECT t.*
FROM trabajador t
WHERE t.activo AND t.es_vendedor
  AND (@branch_id::text = '' OR t.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR t.id = ANY (@sellers))
  AND (@exclude::text = '' OR t.id <> @exclude)
ORDER BY t.nombre;

-- name: SellerByAuthID :one
SELECT * FROM trabajador WHERE auth_user_id = $1;

-- name: SupervisorTerms :many
SELECT gestor_id, supervisor_id, desde, hasta
FROM supervision
WHERE supervisor_id = $1;

-- The inbox: what ingest could not assign or date. It is what stops a file from
-- being lost in silence.
-- name: Inbox :many
SELECT f.*, s.nombre AS source, coalesce(t.nombre, '') AS seller
FROM gpx_file f
JOIN drive_source s ON s.id = f.source_id
LEFT JOIN trabajador t ON t.id = f.trabajador_id
WHERE f.estado IN ('SIN_ASIGNAR', 'SIN_FECHA', 'ERROR')
  AND (@branch_id::text = '' OR f.sucursal_id IS NULL OR f.sucursal_id = @branch_id)
ORDER BY f.importado_at DESC
LIMIT @limit_rows;

-- name: AssignFile :one
UPDATE gpx_file
SET trabajador_id = @seller_id,
    sucursal_id = @branch_id,
    fecha = coalesce(sqlc.narg('fecha')::date, fecha),
    estado = CASE WHEN coalesce(sqlc.narg('fecha')::date, fecha) IS NULL THEN 'SIN_FECHA'::estado_fichero
                  ELSE 'PROCESADO'::estado_fichero END
WHERE id = @id
RETURNING *;

-- name: CreateAlias :one
INSERT INTO device_alias (id, alias, alias_original, trabajador_id, sucursal_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (alias) DO UPDATE SET
    trabajador_id = EXCLUDED.trabajador_id,
    sucursal_id = EXCLUDED.sucursal_id
RETURNING *;

-- name: DeleteAlias :exec
DELETE FROM device_alias WHERE id = $1;

-- name: BranchAliases :many
SELECT a.*, t.nombre AS seller
FROM device_alias a
JOIN trabajador t ON t.id = a.trabajador_id
WHERE @branch_id::text = '' OR a.sucursal_id = @branch_id
ORDER BY a.alias_original;

-- name: RecentScans :many
SELECT * FROM import_log ORDER BY inicio DESC LIMIT $1;

-- name: BranchByAuthOrg :one
SELECT * FROM sucursal WHERE auth_org_id = $1;

-- name: LinkBranchToAuthOrg :exec
-- Atar la sucursal de aquí con la organización de Accesos.
--
-- Las sucursales de aquí nacieron del nombre de la cuenta de Drive y las de Accesos
-- se crearon a mano, así que son las mismas con nombres distintos y sin nada que las
-- una. Se atan la primera vez que entra alguien de esa sucursal, y a partir de ahí la
-- búsqueda es directa.
UPDATE sucursal SET auth_org_id = @auth_org_id, updated_at = now()
WHERE id = @id AND (auth_org_id IS NULL OR auth_org_id = '');

-- ---------------------------------------------------------------------------
-- Por qué no subió
-- ---------------------------------------------------------------------------
--
-- Un día en SIN_FICHERO decía "no hay ruta" y se quedaba callado sobre lo único que
-- hay que hacer con él: si el vendedor no subió nada, si subió y el fichero se
-- atascó, o si lleva un mes sin subir y lo que falla es el GPS. Eso vivía en la
-- pantalla de Administración, que es donde nadie iba a mirarlo.

-- Lo primero de las dos —cuándo subió cada vendedor por última vez— se escribe a
-- mano en upload_state.go: sqlc no acierta a inferir qué es nulo ahí y devolvía
-- `interface{}` en la mitad de las columnas.

-- Los ficheros que SÍ llegaron para un día y no se pudieron usar. Es la diferencia
-- entre "no subió" y "subió y el sistema no supo qué hacer con ello", que son dos
-- conversaciones muy distintas con el vendedor.
-- name: StuckDays :many
SELECT f.trabajador_id::text AS seller_id, f.fecha::date AS fecha, f.estado, count(*)::int AS files
FROM gpx_file f
WHERE f.estado <> 'PROCESADO'
  AND f.trabajador_id IS NOT NULL
  AND f.fecha BETWEEN @from_date::date AND @to_date::date
  AND (@branch_id::text = '' OR f.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR f.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR f.trabajador_id <> @exclude)
GROUP BY f.trabajador_id, f.fecha, f.estado;

-- Los días que entraron A MEDIAS: su fichero llegó cortado y lo que hay es el trozo
-- que se pudo leer, no la jornada.
--
-- Va en su propia consulta y no como una bandera más del calendario porque lo que se
-- necesita aquí es la LISTA —quién, qué día, y qué dijo el error—, para poder ir a
-- pedir que vuelvan a subir esos días concretos.
-- name: TruncatedDays :many
SELECT
    f.trabajador_id::text AS seller_id,
    coalesce(t.nombre, '') AS seller,
    f.fecha::date          AS fecha,
    f.nombre               AS file,
    coalesce(f.error, '')  AS detail,
    f.puntos_total         AS points
FROM gpx_file f
LEFT JOIN trabajador t ON t.id = f.trabajador_id
WHERE f.estado = 'PROCESADO'
  AND f.error IS NOT NULL AND f.error <> ''
  AND f.trabajador_id IS NOT NULL
  AND f.fecha IS NOT NULL
  AND (@branch_id::text = '' OR f.sucursal_id = @branch_id)
ORDER BY f.fecha DESC
LIMIT @limit_rows;
