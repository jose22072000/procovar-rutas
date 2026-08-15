-- Consultas del panel. Todas llevan EL MISMO bloque de alcance:
--
--   (@sucursal_id = ''  OR sucursal_id = @sucursal_id)
--   (cardinality(@trabajadores) = 0 OR trabajador_id = ANY(@trabajadores))
--   (@excluir = ''      OR trabajador_id <> @excluir)
--
-- que es la traducción literal de internal/alcance.Filtro. Se repite en el SQL
-- porque no hay otra forma con sqlc, pero NINGUNA consulta lo construye por su
-- cuenta: los tres parámetros salen siempre de alcance.Calcular. El día que se
-- escriba una consulta nueva sin ellos, la revisión debería rechazarla.

-- El calendario de cumplimiento: la cuadrícula de vendedores × días laborables.
-- name: Calendario :many
SELECT
    d.trabajador_id,
    t.nombre AS trabajador,
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
WHERE d.fecha BETWEEN @desde::date AND @hasta::date
  AND (@sucursal_id::text = '' OR d.sucursal_id = @sucursal_id)
  AND (cardinality(@trabajadores::text[]) = 0 OR d.trabajador_id = ANY (@trabajadores))
  AND (@excluir::text = '' OR d.trabajador_id <> @excluir)
ORDER BY t.nombre, d.fecha;

-- name: ResumenIncidencias :many
SELECT
    d.trabajador_id,
    t.nombre AS trabajador,
    count(*) FILTER (WHERE d.estado = 'SIN_FICHERO')    AS sin_fichero,
    count(*) FILTER (WHERE d.estado = 'SIN_FECHA')      AS sin_fecha,
    count(*) FILTER (WHERE d.estado = 'SIN_MOVIMIENTO') AS sin_movimiento,
    count(*) FILTER (WHERE d.estado = 'OK')             AS dias_ok,
    coalesce(sum(d.km_netos), 0)::float8                AS km_total
FROM track_day d
JOIN trabajador t ON t.id = d.trabajador_id
WHERE d.fecha BETWEEN @desde::date AND @hasta::date
  AND d.estado <> 'NO_LABORABLE'
  AND (@sucursal_id::text = '' OR d.sucursal_id = @sucursal_id)
  AND (cardinality(@trabajadores::text[]) = 0 OR d.trabajador_id = ANY (@trabajadores))
  AND (@excluir::text = '' OR d.trabajador_id <> @excluir)
GROUP BY d.trabajador_id, t.nombre
ORDER BY sin_fichero + sin_fecha + sin_movimiento DESC, t.nombre;

-- name: DiaDeTrabajador :one
SELECT d.*, t.nombre AS trabajador
FROM track_day d
JOIN trabajador t ON t.id = d.trabajador_id
WHERE d.trabajador_id = @trabajador_id AND d.fecha = @fecha::date
  AND (@sucursal_id::text = '' OR d.sucursal_id = @sucursal_id)
  AND (cardinality(@trabajadores::text[]) = 0 OR d.trabajador_id = ANY (@trabajadores))
  AND (@excluir::text = '' OR d.trabajador_id <> @excluir);

-- Los puntos que pinta el visor. Solo la jornada por defecto; el interruptor de
-- "día completo" manda @jornada_inicio = '00:00' y @jornada_fin = '23:59'.
-- name: PuntosDeDia :many
SELECT p.ts, p.lat, p.lon, p.ele, p.speed, p.quality, p.seq
FROM track_point p
JOIN track_day d ON d.trabajador_id = p.trabajador_id
                AND d.fecha = (p.ts AT TIME ZONE @zona::text)::date
WHERE d.id = @track_day_id
  AND p.ts IS NOT NULL
  AND to_char(p.ts AT TIME ZONE @zona::text, 'HH24:MI') BETWEEN @jornada_inicio::text AND @jornada_fin::text
ORDER BY p.ts;

-- Un fichero sin horas no casa con la consulta anterior (no tiene ts), pero su
-- recorrido sí se puede pintar: para eso están estos puntos, por orden de
-- secuencia.
-- name: PuntosDeFichero :many
SELECT p.ts, p.lat, p.lon, p.ele, p.speed, p.quality, p.seq
FROM track_point p
WHERE p.gpx_file_id = $1
ORDER BY p.seq;

-- name: ParadasDeDia :many
SELECT * FROM stop WHERE track_day_id = $1 ORDER BY seq;

-- name: SemanaDeTrabajador :many
SELECT d.*
FROM track_day d
WHERE d.trabajador_id = @trabajador_id
  AND d.fecha BETWEEN @desde::date AND @hasta::date
  AND (@sucursal_id::text = '' OR d.sucursal_id = @sucursal_id)
  AND (cardinality(@trabajadores::text[]) = 0 OR d.trabajador_id = ANY (@trabajadores))
  AND (@excluir::text = '' OR d.trabajador_id <> @excluir)
ORDER BY d.fecha;

-- name: TrabajadoresDelAlcance :many
SELECT t.*
FROM trabajador t
WHERE t.activo AND t.es_vendedor
  AND (@sucursal_id::text = '' OR t.sucursal_id = @sucursal_id)
  AND (cardinality(@trabajadores::text[]) = 0 OR t.id = ANY (@trabajadores))
  AND (@excluir::text = '' OR t.id <> @excluir)
ORDER BY t.nombre;

-- name: TrabajadorPorAuthID :one
SELECT * FROM trabajador WHERE auth_user_id = $1;

-- name: VigenciasDeSupervisor :many
SELECT gestor_id, supervisor_id, desde, hasta
FROM supervision
WHERE supervisor_id = $1;

-- La bandeja: lo que la ingesta no supo asignar o fechar. Es lo que evita que un
-- fichero se pierda en silencio.
-- name: Bandeja :many
SELECT f.*, s.nombre AS fuente
FROM gpx_file f
JOIN drive_source s ON s.id = f.source_id
WHERE f.estado IN ('SIN_ASIGNAR', 'SIN_FECHA', 'ERROR')
  AND (@sucursal_id::text = '' OR f.sucursal_id IS NULL OR f.sucursal_id = @sucursal_id)
ORDER BY f.importado_at DESC
LIMIT @limite;

-- name: AsignarFichero :one
UPDATE gpx_file
SET trabajador_id = @trabajador_id,
    sucursal_id = @sucursal_id,
    fecha = coalesce(sqlc.narg('fecha')::date, fecha),
    estado = CASE WHEN coalesce(sqlc.narg('fecha')::date, fecha) IS NULL THEN 'SIN_FECHA'::estado_fichero
                  ELSE 'PROCESADO'::estado_fichero END
WHERE id = @id
RETURNING *;

-- name: CrearAlias :one
INSERT INTO device_alias (id, alias, alias_original, trabajador_id, sucursal_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (alias) DO UPDATE SET
    trabajador_id = EXCLUDED.trabajador_id,
    sucursal_id = EXCLUDED.sucursal_id
RETURNING *;

-- name: BorrarAlias :exec
DELETE FROM device_alias WHERE id = $1;

-- name: AliasDeSucursal :many
SELECT a.*, t.nombre AS trabajador
FROM device_alias a
JOIN trabajador t ON t.id = a.trabajador_id
WHERE @sucursal_id::text = '' OR a.sucursal_id = @sucursal_id
ORDER BY a.alias_original;

-- name: UltimosBarridos :many
SELECT * FROM import_log ORDER BY inicio DESC LIMIT $1;

-- name: SucursalPorAuthOrg :one
SELECT * FROM sucursal WHERE auth_org_id = $1;
