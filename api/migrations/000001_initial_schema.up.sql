-- procovar-rutas — initial schema.
--
-- Two decisions explain almost everything that follows:
--
--  1. Instants are TIMESTAMPTZ. Trimming to the 9:00–16:00 workday is done by
--     querying `AT TIME ZONE 'America/Havana'`, never by adding hours by hand:
--     Cuba observes daylight saving and the March and November reports would come
--     out shifted during exactly the weeks nobody thinks to double-check them.
--
--  2. track_day exists EVEN WITH NO FILE. A nightly process creates the row for
--     every active seller on every working day; if nothing arrived, it stays as
--     SIN_FICHERO. If an absence were "there is no row", it could not be listed,
--     counted, or sorted by number of misses — which is precisely what the
--     compliance calendar asks for.

-- ---------------------------------------------------------------------------
-- Types
-- ---------------------------------------------------------------------------

CREATE TYPE tipo_fuente AS ENUM ('SUCURSAL', 'VENDEDOR', 'MIXTA');

CREATE TYPE estado_fichero AS ENUM ('PROCESADO', 'SIN_ASIGNAR', 'SIN_FECHA', 'ERROR');

-- Where the day's date came from. A day dated from the file name is not worth the
-- same as one dated from its own points, and the panel says so.
CREATE TYPE origen_fecha AS ENUM ('PUNTOS', 'NOMBRE', 'DRIVE', 'NINGUNO');

-- Why a point does not count. It is flagged, NOT deleted: if the threshold turns
-- out to be wrong tomorrow, it is recomputed without downloading from Drive again.
CREATE TYPE calidad_punto AS ENUM ('OK', 'SALTO', 'IMPRECISO', 'DUPLICADO', 'SIN_HORA');

CREATE TYPE estado_dia AS ENUM (
    'OK', 'SIN_FICHERO', 'SIN_FECHA', 'SIN_MOVIMIENTO', 'MOVIMIENTO_ESCASO', 'NO_LABORABLE'
);

-- ---------------------------------------------------------------------------
-- People and structure
-- ---------------------------------------------------------------------------

-- Local mirror of procovar-auth's Organization: only what is needed here to query
-- quickly; identity is owned over there.
CREATE TABLE sucursal (
    id          TEXT PRIMARY KEY,
    auth_org_id TEXT UNIQUE,
    nombre      TEXT NOT NULL,
    activa      BOOLEAN NOT NULL DEFAULT TRUE,
    timezone    TEXT NOT NULL DEFAULT 'America/Havana',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- auth_user_id is NULL while someone only shows up in the GPX files and has no
-- account yet: that happens early on, while the aliases are being matched.
CREATE TABLE trabajador (
    id           TEXT PRIMARY KEY,
    auth_user_id TEXT UNIQUE,
    nombre       TEXT NOT NULL,
    sucursal_id  TEXT NOT NULL REFERENCES sucursal (id),
    es_vendedor  BOOLEAN NOT NULL DEFAULT TRUE,
    activo       BOOLEAN NOT NULL DEFAULT TRUE,
    -- Joining and leaving: someone who started in March must not come out as absent in January.
    desde        DATE NOT NULL DEFAULT CURRENT_DATE,
    hasta        DATE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX trabajador_sucursal_idx ON trabajador (sucursal_id, activo);

-- Who supervises whom, WITH TERMS.
--
-- Deliberately not a supervisor_id column on trabajador: the panel queries past
-- weeks and has to be able to answer "who supervised this seller in August" even
-- if by October they have changed team. With a column, the history falsifies
-- itself every time someone changes boss.
CREATE TABLE supervision (
    id            TEXT PRIMARY KEY,
    gestor_id     TEXT NOT NULL REFERENCES trabajador (id),
    supervisor_id TEXT NOT NULL REFERENCES trabajador (id),
    desde         DATE NOT NULL,
    hasta         DATE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT supervision_rango_valido CHECK (hasta IS NULL OR hasta >= desde),
    CONSTRAINT supervision_no_a_si_mismo CHECK (gestor_id <> supervisor_id)
);

CREATE INDEX supervision_supervisor_idx ON supervision (supervisor_id, desde, hasta);
CREATE INDEX supervision_gestor_idx ON supervision (gestor_id, desde, hasta);

-- ---------------------------------------------------------------------------
-- Where the data comes from: Drive folders
-- ---------------------------------------------------------------------------

-- A Drive folder scanned for .gpx files.
--
-- The real setup: THERE IS ONE GOOGLE ACCOUNT PER BRANCH, each named after its
-- branch. Inside, one folder per GPS profile — named after the seller or the
-- tablet — and inside that, the `YYYYMMDD.gpx` files.
--
-- That is why each source says WHICH credential reads it (`credencial`, the
-- account key in the configuration). If tomorrow every folder is shared into a
-- single account, it is enough for all sources to point at the same one.
CREATE TABLE drive_source (
    id             TEXT PRIMARY KEY,
    nombre         TEXT NOT NULL,
    folder_id      TEXT NOT NULL UNIQUE,
    tipo           tipo_fuente NOT NULL DEFAULT 'SUCURSAL',
    sucursal_id    TEXT REFERENCES sucursal (id),
    -- When tipo = VENDEDOR, who everything landing here belongs to.
    trabajador_id  TEXT REFERENCES trabajador (id),
    -- Key of the Google account this folder is read with.
    credencial     TEXT NOT NULL DEFAULT 'principal',
    activa         BOOLEAN NOT NULL DEFAULT TRUE,
    -- Incremental scan cursor: last modifiedTime seen. The nightly sweep ignores
    -- it on purpose and walks the whole folder.
    cursor_modificado TIMESTAMPTZ,
    ultimo_barrido TIMESTAMPTZ,
    ultimo_error   TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX drive_source_activa_idx ON drive_source (activa);

-- A record of every .gpx seen in Drive. It is the system's "I already did this":
-- files are NOT moved or deleted from Drive (unlike PEDIDO's ingest), because
-- those folders are seen by the sellers themselves.
CREATE TABLE gpx_file (
    id               TEXT PRIMARY KEY,
    source_id        TEXT NOT NULL REFERENCES drive_source (id),
    drive_file_id    TEXT NOT NULL UNIQUE,
    -- sha256 of the content: if the same file appears in two folders it is ingested
    -- once; if it is re-uploaded after a fix, the hash changes.
    sha256           TEXT NOT NULL UNIQUE,
    nombre           TEXT NOT NULL,
    ruta_carpeta     TEXT,
    tamano_bytes     INTEGER,
    drive_created_at TIMESTAMPTZ,
    estado           estado_fichero NOT NULL,
    error            TEXT,
    trabajador_id    TEXT REFERENCES trabajador (id),
    sucursal_id      TEXT REFERENCES sucursal (id),
    -- The day it belongs to, in the branch's local date.
    fecha            DATE,
    origen_fecha     origen_fecha NOT NULL DEFAULT 'NINGUNO',
    puntos_total     INTEGER NOT NULL DEFAULT 0,
    puntos_validos   INTEGER NOT NULL DEFAULT 0,
    primer_fix       TIMESTAMPTZ,
    ultimo_fix       TIMESTAMPTZ,
    -- Raw text the seller was inferred from, so the admin can see in the inbox
    -- WHAT has to be matched.
    pista_alias      TEXT,
    importado_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX gpx_file_estado_idx ON gpx_file (estado);
CREATE INDEX gpx_file_trabajador_fecha_idx ON gpx_file (trabajador_id, fecha);
CREATE INDEX gpx_file_source_idx ON gpx_file (source_id, importado_at);

-- ---------------------------------------------------------------------------
-- Los datos GPS
-- ---------------------------------------------------------------------------

CREATE TABLE track_point (
    -- BIGSERIAL and not TEXT: this is the table that really grows (~141k rows a
    -- day with 8 branches), and past a year it is worth partitioning by month.
    id            BIGSERIAL PRIMARY KEY,
    gpx_file_id   TEXT NOT NULL REFERENCES gpx_file (id) ON DELETE CASCADE,
    trabajador_id TEXT REFERENCES trabajador (id),
    sucursal_id   TEXT REFERENCES sucursal (id),
    -- Instant in UTC. Filtering by workday is done with AT TIME ZONE.
    ts            TIMESTAMPTZ,
    lat           DOUBLE PRECISION NOT NULL,
    lon           DOUBLE PRECISION NOT NULL,
    ele           DOUBLE PRECISION,
    speed         DOUBLE PRECISION,
    accuracy      DOUBLE PRECISION,
    -- Order within the file: keeps the sequence even when times are missing.
    seq           INTEGER NOT NULL,
    quality       calidad_punto NOT NULL DEFAULT 'OK'
);

CREATE INDEX track_point_trabajador_ts_idx ON track_point (trabajador_id, ts);
CREATE INDEX track_point_sucursal_ts_idx ON track_point (sucursal_id, ts);
CREATE INDEX track_point_fichero_idx ON track_point (gpx_file_id, seq);

-- One seller, one day. Precomputed during ingest: the panel and the report cannot
-- depend on scanning 18,000 points every time someone opens a screen.
-- pantalla.
CREATE TABLE track_day (
    id               TEXT PRIMARY KEY,
    trabajador_id    TEXT NOT NULL REFERENCES trabajador (id),
    sucursal_id      TEXT NOT NULL REFERENCES sucursal (id),
    fecha            DATE NOT NULL,
    estado           estado_dia NOT NULL,
    primer_fix       TIMESTAMPTZ,
    ultimo_fix       TIMESTAMPTZ,
    km_netos         DOUBLE PRECISION NOT NULL DEFAULT 0,
    min_movimiento   INTEGER NOT NULL DEFAULT 0,
    min_parado       INTEGER NOT NULL DEFAULT 0,
    cobertura        DOUBLE PRECISION NOT NULL DEFAULT 0,
    huecos           INTEGER NOT NULL DEFAULT 0,
    puntos           INTEGER NOT NULL DEFAULT 0,
    -- Greatest distance to the centroid, in metres: the signal that decides
    -- stillness, because it is the only one GPS noise does not corrupt.
    radio_dispersion DOUBLE PRECISION,
    centroide_lat    DOUBLE PRECISION,
    centroide_lon    DOUBLE PRECISION,
    -- Where the still day was spent: the fact you actually want when opening the case.
    lugar_texto      TEXT,
    banderas         TEXT[] NOT NULL DEFAULT '{}',
    -- NULL when the status is SIN_FICHERO: there is no file to point at.
    gpx_file_id      TEXT REFERENCES gpx_file (id) ON DELETE SET NULL,
    calculado_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT track_day_unico UNIQUE (trabajador_id, fecha)
);

CREATE INDEX track_day_sucursal_fecha_idx ON track_day (sucursal_id, fecha);
CREATE INDEX track_day_fecha_estado_idx ON track_day (fecha, estado);

CREATE TABLE stop (
    id                  TEXT PRIMARY KEY,
    track_day_id        TEXT NOT NULL REFERENCES track_day (id) ON DELETE CASCADE,
    trabajador_id       TEXT NOT NULL REFERENCES trabajador (id),
    sucursal_id         TEXT NOT NULL REFERENCES sucursal (id),
    inicio              TIMESTAMPTZ NOT NULL,
    fin                 TIMESTAMPTZ NOT NULL,
    duracion_min        INTEGER NOT NULL,
    lat                 DOUBLE PRECISION NOT NULL,
    lon                 DOUBLE PRECISION NOT NULL,
    radio               DOUBLE PRECISION,
    -- Nearest client, when the cross-reference with client geolocation is enabled.
    cliente_ref         TEXT,
    cliente_nombre      TEXT,
    distancia_cliente_m DOUBLE PRECISION,
    seq                 INTEGER NOT NULL
);

CREATE INDEX stop_trabajador_idx ON stop (trabajador_id, inicio);
CREATE INDEX stop_dia_idx ON stop (track_day_id, seq);

-- Device alias → seller. It is what makes resolution robust without knowing in
-- advance what the files are called: the admin matches an alias once from the
-- inbox and never touches it again.
CREATE TABLE device_alias (
    id             TEXT PRIMARY KEY,
    -- Normalized (lowercase, no accents, no separators) so it really matches.
    alias          TEXT NOT NULL UNIQUE,
    alias_original TEXT NOT NULL,
    trabajador_id  TEXT NOT NULL REFERENCES trabajador (id) ON DELETE CASCADE,
    sucursal_id    TEXT REFERENCES sucursal (id),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     TEXT
);

-- ---------------------------------------------------------------------------
-- Configuración
-- ---------------------------------------------------------------------------

-- Per-branch thresholds. Everything configurable on purpose: a seller working the
-- city is not the same as one covering three municipalities, and the good values
-- will only be known once there is real data.
CREATE TABLE sucursal_config (
    id                     TEXT PRIMARY KEY,
    sucursal_id            TEXT NOT NULL UNIQUE REFERENCES sucursal (id) ON DELETE CASCADE,
    jornada_inicio         TEXT NOT NULL DEFAULT '09:00',
    jornada_fin            TEXT NOT NULL DEFAULT '16:00',
    -- 1 = lunes … 7 = domingo. Por defecto, lunes a viernes.
    dias_laborables        INTEGER[] NOT NULL DEFAULT '{1,2,3,4,5}',
    parada_radio_m         INTEGER NOT NULL DEFAULT 60,
    parada_minutos         INTEGER NOT NULL DEFAULT 5,
    -- Noise floor: legs shorter than this add no kilometres.
    paso_minimo_m          INTEGER NOT NULL DEFAULT 25,
    -- Stillness: spread radius + the minimum workday needed to claim it.
    sin_mov_radio_m        INTEGER NOT NULL DEFAULT 300,
    sin_mov_span_min       INTEGER NOT NULL DEFAULT 120,
    escaso_km_netos        DOUBLE PRECISION NOT NULL DEFAULT 5,
    max_velocidad_kmh      INTEGER NOT NULL DEFAULT 150,
    max_accuracy_m         INTEGER NOT NULL DEFAULT 100,
    hueco_minutos          INTEGER NOT NULL DEFAULT 30,
    cobertura_gap_min      INTEGER NOT NULL DEFAULT 5,
    cobertura_minima       DOUBLE PRECISION NOT NULL DEFAULT 70,
    tolerancia_entrada_min INTEGER NOT NULL DEFAULT 15,
    tolerancia_salida_min  INTEGER NOT NULL DEFAULT 15,
    visita_radio_m         INTEGER NOT NULL DEFAULT 80
);

-- Without this, the 1st of May would show up as the whole workforce absent.
CREATE TABLE feriado (
    id          TEXT PRIMARY KEY,
    fecha       DATE NOT NULL,
    nombre      TEXT NOT NULL,
    -- NULL = national holiday, applies to every branch.
    sucursal_id TEXT REFERENCES sucursal (id)
);

CREATE UNIQUE INDEX feriado_unico_idx
    ON feriado (fecha, COALESCE(sucursal_id, ''));

-- An audit of each scan: what was read, what went in and what failed.
CREATE TABLE import_log (
    id                TEXT PRIMARY KEY,
    source_id         TEXT REFERENCES drive_source (id),
    tipo              TEXT NOT NULL, -- incremental | nocturno | backfill | manual
    inicio            TIMESTAMPTZ NOT NULL DEFAULT now(),
    fin               TIMESTAMPTZ,
    ficheros_vistos   INTEGER NOT NULL DEFAULT 0,
    ficheros_nuevos   INTEGER NOT NULL DEFAULT 0,
    ficheros_error    INTEGER NOT NULL DEFAULT 0,
    puntos_insertados INTEGER NOT NULL DEFAULT 0,
    ok                BOOLEAN NOT NULL DEFAULT FALSE,
    detalle           TEXT
);

CREATE INDEX import_log_inicio_idx ON import_log (inicio DESC);
