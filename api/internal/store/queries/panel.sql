-- Consultas del panel. Todas llevan EL MISMO bloque de alcance:
--
--   (@branch_id = ''  OR sucursal_id = @branch_id)
--   (cardinality(@sellers) = 0 OR trabajador_id = ANY(@sellers))
--   (@exclude = ''      OR trabajador_id <> @exclude)
--
-- que es la traducción literal de internal/alcance.Filtro. Se repite en el SQL
-- porque no hay otra forma con sqlc, pero NINGUNA consulta lo construye por su
-- cuenta: los tres parámetros salen siempre de alcance.Calcular. El día que se
-- escriba una consulta nueva sin ellos, la revisión debería rechazarla.

-- El calendario de cumplimiento: la cuadrícula de vendedores × días laborables.
-- name: Calendar :many
SELECT
    d.trabajador_id,
    t.nombre AS seller,
    d.sucursal_id,
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
-- Postgres NO admite un alias de salida dentro de una expresión del ORDER BY
-- (sí suelto, no sumado), así que los conteos se repiten aquí. Escribirlo con
-- los alias compila en sqlc y revienta en la base: "column ... does not exist".
ORDER BY count(*) FILTER (WHERE d.estado = 'SIN_FICHERO')
       + count(*) FILTER (WHERE d.estado = 'SIN_FECHA')
       + count(*) FILTER (WHERE d.estado = 'SIN_MOVIMIENTO') DESC,
         t.nombre;

-- name: SellerDay :one
SELECT d.*, t.nombre AS seller
FROM track_day d
JOIN trabajador t ON t.id = d.trabajador_id
WHERE d.trabajador_id = @seller_id AND d.fecha = @date::date
  AND (@branch_id::text = '' OR d.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR d.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR d.trabajador_id <> @exclude);

-- Los puntos que pinta el visor. Solo la jornada por defecto; el interruptor de
-- "día completo" manda @workday_start = '00:00' y @workday_end = '23:59'.
-- name: DayPoints :many
SELECT p.ts, p.lat, p.lon, p.ele, p.speed, p.quality, p.seq
FROM track_point p
JOIN track_day d ON d.trabajador_id = p.trabajador_id
                AND d.fecha = (p.ts AT TIME ZONE @zone::text)::date
WHERE d.id = @track_day_id
  AND p.ts IS NOT NULL
  AND to_char(p.ts AT TIME ZONE @zone::text, 'HH24:MI') BETWEEN @workday_start::text AND @workday_end::text
ORDER BY p.ts;

-- Un fichero sin horas no casa con la consulta anterior (no tiene ts), pero su
-- recorrido sí se puede pintar: para eso están estos puntos, por orden de
-- secuencia.
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

-- La bandeja: lo que la ingesta no supo asignar o fechar. Es lo que evita que un
-- fichero se pierda en silencio.
-- name: Inbox :many
SELECT f.*, s.nombre AS source
FROM gpx_file f
JOIN drive_source s ON s.id = f.source_id
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
