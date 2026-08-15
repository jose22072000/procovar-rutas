-- Consultas de la ingesta: leer las carpetas, registrar ficheros y volcar puntos.

-- name: FuentesActivas :many
SELECT * FROM drive_source WHERE activa ORDER BY nombre;

-- name: FuentePorID :one
SELECT * FROM drive_source WHERE id = $1;

-- name: CrearFuente :one
INSERT INTO drive_source (id, nombre, folder_id, tipo, sucursal_id, trabajador_id, credencial)
VALUES (@id, @nombre, @folder_id, @tipo::tipo_fuente, @sucursal_id, @trabajador_id,
        COALESCE(NULLIF(@credencial::text, ''), 'principal'))
RETURNING *;

-- name: ActualizarCursorFuente :exec
UPDATE drive_source
SET cursor_modificado = $2, ultimo_barrido = now(), ultimo_error = NULL, updated_at = now()
WHERE id = $1;

-- name: MarcarErrorFuente :exec
UPDATE drive_source
SET ultimo_barrido = now(), ultimo_error = $2, updated_at = now()
WHERE id = $1;

-- El "ya procesé esto" del sistema. Se consulta por las dos claves porque un
-- mismo contenido puede aparecer con otro drive_file_id (copiado a otra carpeta)
-- y un mismo drive_file_id puede cambiar de contenido (re-subida corregida).
-- name: FicheroPorDriveID :one
SELECT * FROM gpx_file WHERE drive_file_id = $1;

-- name: FicheroPorSha :one
SELECT * FROM gpx_file WHERE sha256 = $1;

-- name: GuardarFichero :one
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

-- name: BorrarPuntosDeFichero :exec
DELETE FROM track_point WHERE gpx_file_id = $1;

-- name: TrabajadoresActivos :many
SELECT * FROM trabajador WHERE activo ORDER BY nombre;

-- name: AliasTodos :many
SELECT alias, trabajador_id, sucursal_id FROM device_alias;

-- name: ConfigDeSucursal :one
SELECT * FROM sucursal_config WHERE sucursal_id = $1;

-- name: SucursalPorID :one
SELECT * FROM sucursal WHERE id = $1;

-- name: FeriadosDeSucursal :many
SELECT fecha FROM feriado
WHERE sucursal_id IS NULL OR sucursal_id = $1;

-- El día se reemplaza entero en cada recálculo: es idempotente, así que
-- reprocesar un fichero cuantas veces haga falta no duplica nada.
-- name: GuardarDia :one
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

-- name: BorrarParadasDeDia :exec
DELETE FROM stop WHERE track_day_id = $1;

-- name: CrearParada :exec
INSERT INTO stop (
    id, track_day_id, trabajador_id, sucursal_id, inicio, fin, duracion_min,
    lat, lon, radio, cliente_ref, cliente_nombre, distancia_cliente_m, seq
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- Marca las ausencias del día laborable: cada vendedor activo sin fila queda en
-- SIN_FICHERO. Sin esto, "no subió nada" sería la ausencia de una fila, y las
-- ausencias no se pueden listar, ni contar, ni ordenar por número de faltas.
-- name: MarcarAusencias :execrows
INSERT INTO track_day (id, trabajador_id, sucursal_id, fecha, estado)
SELECT
    md5(t.id || ':' || to_char(@fecha::date, 'YYYY-MM-DD')),
    t.id,
    t.sucursal_id,
    @fecha::date,
    'SIN_FICHERO'
FROM trabajador t
WHERE t.activo
  AND t.es_vendedor
  AND t.desde <= @fecha::date
  AND (t.hasta IS NULL OR t.hasta >= @fecha::date)
  AND (@sucursal_id::text = '' OR t.sucursal_id = @sucursal_id)
ON CONFLICT (trabajador_id, fecha) DO NOTHING;

-- name: AbrirImportLog :one
INSERT INTO import_log (id, source_id, tipo) VALUES ($1, $2, $3) RETURNING *;

-- name: CerrarImportLog :exec
UPDATE import_log
SET fin = now(), ficheros_vistos = $2, ficheros_nuevos = $3, ficheros_error = $4,
    puntos_insertados = $5, ok = $6, detalle = $7
WHERE id = $1;

-- Todos los puntos de un vendedor en un día local, vengan del fichero que
-- vengan: el día se recalcula desde la base y no desde el fichero recién
-- llegado, porque puede haber varios ficheros para el mismo día (una sesión de
-- mañana y otra de tarde) y sumarlos por separado daría dos veredictos.
-- name: PuntosDeTrabajadorEnFecha :many
SELECT p.ts, p.lat, p.lon, p.accuracy, p.seq
FROM track_point p
WHERE p.trabajador_id = @trabajador_id
  AND p.ts IS NOT NULL
  AND (p.ts AT TIME ZONE @zona::text)::date = @fecha::date
ORDER BY p.ts;

-- name: SucursalesActivas :many
SELECT * FROM sucursal WHERE activa ORDER BY nombre;

-- name: TrabajadorPorID :one
SELECT * FROM trabajador WHERE id = $1;

-- Altas mínimas, solo para las pruebas de integración: el alta real de
-- sucursales y trabajadores viene de procovar-auth, no de aquí.
-- name: CrearSucursalDePrueba :one
INSERT INTO sucursal (id, nombre, auth_org_id) VALUES ($1, $2, $3) RETURNING *;

-- name: CrearTrabajadorDePrueba :one
INSERT INTO trabajador (id, nombre, sucursal_id, auth_user_id, desde)
VALUES ($1, $2, $3, $4, COALESCE(sqlc.narg('desde')::date, DATE '2020-01-01'))
RETURNING *;
