-- Ingest queries: read the folders, record files and bulk-load points.

-- name: ActiveSources :many
SELECT * FROM drive_source WHERE activa ORDER BY nombre;

-- name: ActiveSourcesWithBranch :many
-- Igual, pero con el nombre de la sucursal: es lo que hay que poder ver en
-- Administración para saber si la ingesta repartió cada carpeta donde tocaba.
SELECT f.*, coalesce(s.nombre, '') AS branch
FROM drive_source f
LEFT JOIN sucursal s ON s.id = f.sucursal_id
WHERE f.activa
ORDER BY coalesce(s.nombre, 'zzz'), f.nombre;

-- name: SourceByID :one
SELECT * FROM drive_source WHERE id = $1;

-- name: CreateSource :one
INSERT INTO drive_source (id, nombre, folder_id, tipo, sucursal_id, trabajador_id, credencial)
VALUES (@id, @name, @folder_id, @type::tipo_fuente, @branch_id, @seller_id,
        COALESCE(NULLIF(@credential::text, ''), 'principal'))
RETURNING *;

-- name: UpdateSourceCursor :exec
UPDATE drive_source
SET cursor_modificado = $2, ultimo_barrido = now(), ultimo_error = NULL, updated_at = now()
WHERE id = $1;

-- name: MarkSourceError :exec
UPDATE drive_source
SET ultimo_barrido = now(), ultimo_error = $2, updated_at = now()
WHERE id = $1;

-- The system's "I already did this". It is checked on both keys because the same
-- content can turn up under another drive_file_id (copied to another folder) and
-- the same drive_file_id can change content (re-uploaded after a fix).
-- name: FileByDriveID :one
SELECT * FROM gpx_file WHERE drive_file_id = $1;

-- name: FileBySha :one
SELECT * FROM gpx_file WHERE sha256 = $1;

-- name: SaveFile :one
INSERT INTO gpx_file (
    id, source_id, drive_file_id, sha256, nombre, ruta_carpeta, tamano_bytes,
    drive_created_at, estado, error, trabajador_id, sucursal_id, fecha,
    origen_fecha, puntos_total, puntos_validos, primer_fix, ultimo_fix, pista_alias
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (drive_file_id) DO UPDATE SET
    sha256 = EXCLUDED.sha256,
    nombre = EXCLUDED.nombre,
    ruta_carpeta = EXCLUDED.ruta_carpeta,
    tamano_bytes = EXCLUDED.tamano_bytes,
    estado = EXCLUDED.estado,
    error = EXCLUDED.error,
    trabajador_id = EXCLUDED.trabajador_id,
    sucursal_id = EXCLUDED.sucursal_id,
    fecha = EXCLUDED.fecha,
    origen_fecha = EXCLUDED.origen_fecha,
    puntos_total = EXCLUDED.puntos_total,
    puntos_validos = EXCLUDED.puntos_validos,
    primer_fix = EXCLUDED.primer_fix,
    ultimo_fix = EXCLUDED.ultimo_fix,
    pista_alias = EXCLUDED.pista_alias,
    importado_at = now()
RETURNING *;

-- name: DeleteFilePoints :exec
DELETE FROM track_point WHERE gpx_file_id = $1;

-- name: ActiveSellers :many
SELECT * FROM trabajador WHERE activo ORDER BY nombre;

-- name: AllAliases :many
SELECT alias, trabajador_id, sucursal_id FROM device_alias;

-- name: BranchConfig :one
SELECT * FROM sucursal_config WHERE sucursal_id = $1;

-- name: BranchByID :one
SELECT * FROM sucursal WHERE id = $1;

-- name: BranchHolidays :many
SELECT fecha FROM feriado
WHERE sucursal_id IS NULL OR sucursal_id = $1;

-- The day is replaced whole on every recompute: it is idempotent, so reprocessing
-- a file as many times as needed duplicates nothing.
-- name: SaveDay :one
INSERT INTO track_day (
    id, trabajador_id, sucursal_id, fecha, estado, primer_fix, ultimo_fix,
    km_netos, min_movimiento, min_parado, cobertura, huecos, puntos,
    radio_dispersion, centroide_lat, centroide_lon, lugar_texto, banderas, gpx_file_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (trabajador_id, fecha) DO UPDATE SET
    estado = EXCLUDED.estado,
    primer_fix = EXCLUDED.primer_fix,
    ultimo_fix = EXCLUDED.ultimo_fix,
    km_netos = EXCLUDED.km_netos,
    min_movimiento = EXCLUDED.min_movimiento,
    min_parado = EXCLUDED.min_parado,
    cobertura = EXCLUDED.cobertura,
    huecos = EXCLUDED.huecos,
    puntos = EXCLUDED.puntos,
    radio_dispersion = EXCLUDED.radio_dispersion,
    centroide_lat = EXCLUDED.centroide_lat,
    centroide_lon = EXCLUDED.centroide_lon,
    lugar_texto = EXCLUDED.lugar_texto,
    banderas = EXCLUDED.banderas,
    gpx_file_id = EXCLUDED.gpx_file_id,
    calculado_at = now()
RETURNING *;

-- name: DeleteDayStops :exec
DELETE FROM stop WHERE track_day_id = $1;

-- name: CreateStop :exec
INSERT INTO stop (
    id, track_day_id, trabajador_id, sucursal_id, inicio, fin, duracion_min,
    lat, lon, radio, cliente_ref, cliente_nombre, distancia_cliente_m, seq
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- Marks the working day's absences: every active seller with no row ends up as
-- SIN_FICHERO. Without this, "uploaded nothing" would be the absence of a row, and
-- absences cannot be listed, counted, or sorted by number of misses.
-- name: MarkAbsences :execrows
INSERT INTO track_day (id, trabajador_id, sucursal_id, fecha, estado)
SELECT
    md5(t.id || ':' || to_char(@date::date, 'YYYY-MM-DD')),
    t.id,
    t.sucursal_id,
    @date::date,
    'SIN_FICHERO'
FROM trabajador t
WHERE t.activo
  AND t.es_vendedor
  AND t.desde <= @date::date
  AND (t.hasta IS NULL OR t.hasta >= @date::date)
  AND (@branch_id::text = '' OR t.sucursal_id = @branch_id)
ON CONFLICT (trabajador_id, fecha) DO NOTHING;

-- name: OpenImportLog :one
INSERT INTO import_log (id, source_id, tipo) VALUES ($1, $2, $3) RETURNING *;

-- name: CloseImportLog :exec
UPDATE import_log
SET fin = now(), ficheros_vistos = $2, ficheros_nuevos = $3, ficheros_error = $4,
    puntos_insertados = $5, ok = $6, detalle = $7
WHERE id = $1;

-- Every point of a seller on a local day, whichever file it came from: the day is
-- recomputed from the database and not from the file that just arrived, because
-- there can be several files for the same day (a morning session and an afternoon
-- one) and adding them separately would give two verdicts.
-- name: SellerPointsOnDate :many
SELECT p.ts, p.lat, p.lon, p.accuracy, p.seq
FROM track_point p
WHERE p.trabajador_id = @seller_id
  AND p.ts IS NOT NULL
  AND (p.ts AT TIME ZONE @zone::text)::date = @date::date
ORDER BY p.ts;

-- name: ActiveBranches :many
SELECT * FROM sucursal WHERE activa ORDER BY nombre;

-- name: SellerByID :one
SELECT * FROM trabajador WHERE id = $1;

-- Minimal inserts, for the integration tests only: branches and sellers are really
-- created from procovar-auth, not from here.
-- name: CreateTestBranch :one
-- La clave se calcula aquí para no tener que repetirla en cada prueba: es la misma
-- normalización que hace el código (sin tildes, sin espacios, minúsculas).
INSERT INTO sucursal (id, nombre, auth_org_id, clave)
VALUES ($1, $2, $3,
    regexp_replace(lower(translate($2, 'áéíóúüñÁÉÍÓÚÜÑ', 'aeiouunAEIOUUN')), '[^a-z0-9]', '', 'g'))
RETURNING *;

-- name: CreateTestSeller :one
INSERT INTO trabajador (id, nombre, sucursal_id, auth_user_id, desde)
VALUES ($1, $2, $3, $4, COALESCE(sqlc.narg('desde')::date, DATE '2020-01-01'))
RETURNING *;

-- Find-or-create by name, for the folder-name-is-the-seller flow.
--
-- Each shared Drive folder IS a seller's GPS profile, so its name identifies them
-- well enough for what the panel is for: a manager looks at their branch's GPS and
-- already knows who is who. Making somebody match every folder to a person by hand
-- would be asking them to type in what the folder already says.

-- name: BranchByKey :one
-- La sucursal se busca por su CLAVE, no por su nombre: la misma cuenta viene escrita
-- de varias formas ("Camagüey Procovar", "camaguey.procovar@…", "santiagoprocovar")
-- y todas tienen que caer en la misma fila.
SELECT * FROM sucursal WHERE clave = @key LIMIT 1;

-- name: CreateBranchByKey :one
-- Si dos empujes llegan a la vez, el segundo choca con el índice único y se queda con
-- la que creó el primero, en vez de abrir otra. Así aparecieron siete "Guantánamo".
INSERT INTO sucursal (id, nombre, clave) VALUES (@id, @name, @key)
-- Si ya existe, se queda con el nombre que mejor se lee: primero el que NO arrastra
-- el apellido de la empresa, y a igualdad, el más largo ("Las Tunas" gana a
-- "lastunas", y "Santiago" gana a "santiagoprocovar" aunque sea más corto).
ON CONFLICT (clave) DO UPDATE SET nombre = CASE
    WHEN sucursal.nombre ~* 'procovar$' AND EXCLUDED.nombre !~* 'procovar$' THEN EXCLUDED.nombre
    WHEN (sucursal.nombre ~* 'procovar$') = (EXCLUDED.nombre ~* 'procovar$')
         AND length(EXCLUDED.nombre) > length(sucursal.nombre) THEN EXCLUDED.nombre
    ELSE sucursal.nombre
END
RETURNING *;

-- name: SellerByNameInBranch :one
SELECT * FROM trabajador WHERE nombre = @name AND sucursal_id = @branch_id LIMIT 1;

-- name: CreateSellerByName :one
INSERT INTO trabajador (id, nombre, sucursal_id) VALUES (@id, @name, @branch_id)
ON CONFLICT DO NOTHING
RETURNING *;

-- name: IngestStats :one
-- Un vistazo al estado de la ingesta, para vigilarla desde fuera sin abrir la base.
SELECT
    (SELECT count(*) FROM gpx_file)                                   AS files,
    (SELECT count(*) FROM gpx_file WHERE estado = 'PROCESADO')        AS files_ok,
    (SELECT count(*) FROM gpx_file WHERE estado = 'SIN_ASIGNAR')      AS files_unassigned,
    (SELECT count(*) FROM gpx_file WHERE estado = 'SIN_FECHA')        AS files_no_date,
    (SELECT count(*) FROM gpx_file WHERE estado = 'ERROR')            AS files_failed,
    (SELECT count(*) FROM track_point)                                AS points,
    (SELECT count(*) FROM track_day)                                  AS days,
    (SELECT count(*) FROM trabajador)                                 AS sellers,
    (SELECT count(*) FROM sucursal)                                   AS branches,
    (SELECT max(importado_at) FROM gpx_file)                          AS last_file;

-- name: BranchBreakdown :many
-- Qué sucursales se crearon y cuánto tiene cada una. Es lo que dice si el reparto
-- por sucursal está funcionando o si todo cayó en el mismo saco.
SELECT
    s.nombre AS branch,
    (SELECT count(*) FROM trabajador t WHERE t.sucursal_id = s.id) AS sellers,
    (SELECT count(*) FROM gpx_file f WHERE f.sucursal_id = s.id)   AS files
FROM sucursal s
ORDER BY s.nombre;

-- name: SetSourceBranch :exec
UPDATE drive_source SET sucursal_id = @branch_id, credencial = @credential, updated_at = now()
WHERE id = @id;

-- name: MoveSourceDataToBranch :exec
-- Lleva a su sucursal lo que ya entró por una carpeta: sus ficheros, los vendedores
-- que salieron de ella y los días de esos vendedores.
--
-- Hace falta porque las carpetas se dieron de alta con una credencial de relleno y
-- todo cayó en una sucursal llamada "principal". Sin esto habría que borrar y volver
-- a ingerir, y los ficheros ya están apartados en Drive: no volverían a entrar.
WITH afectados AS (
    SELECT DISTINCT f.trabajador_id
    FROM gpx_file f
    WHERE f.source_id = @source_id AND f.trabajador_id IS NOT NULL
),
mueve_ficheros AS (
    UPDATE gpx_file SET sucursal_id = @branch_id WHERE source_id = @source_id
    RETURNING 1
),
mueve_dias AS (
    UPDATE track_day SET sucursal_id = @branch_id
    WHERE trabajador_id IN (SELECT trabajador_id FROM afectados)
    RETURNING 1
)
-- Los PUNTOS no se tocan a propósito. Son millones y su sucursal no la mira nadie:
-- el panel filtra por track_day y por gpx_file. Actualizarlos aquí convertía una
-- reasignación instantánea en minutos de espera, con n8n clavado esperando la
-- respuesta.
UPDATE trabajador SET sucursal_id = @branch_id, updated_at = now()
WHERE id IN (SELECT trabajador_id FROM afectados);

-- name: KnownFiles :many
-- De esta lista, cuáles ya están dentro. Es lo que permite a la ingesta saltarse lo
-- hecho sin tener que mover nada en Drive.
SELECT drive_file_id FROM gpx_file WHERE drive_file_id = ANY(@ids::text[]);
