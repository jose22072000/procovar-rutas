-- procovar-rutas — esquema inicial.
--
-- Dos decisiones explican casi todo lo que sigue:
--
--  1. Los instantes son TIMESTAMPTZ. El recorte de la jornada 9:00–16:00 se hace
--     consultando `AT TIME ZONE 'America/Havana'`, nunca sumando horas a mano:
--     Cuba tiene horario de verano y los reportes de marzo y noviembre saldrían
--     corridos justo en las semanas en que a nadie se le ocurre revisarlos.
--
--  2. track_day existe AUNQUE NO HAYA FICHERO. Un proceso nocturno crea la fila
--     de cada vendedor activo por cada día laborable; si no llegó nada, queda en
--     SIN_FICHERO. Si la ausencia fuese "no hay fila", no se podría listar, ni
--     contar, ni ordenar por número de faltas — que es justo lo que pide el
--     calendario de cumplimiento.

-- ---------------------------------------------------------------------------
-- Tipos
-- ---------------------------------------------------------------------------

CREATE TYPE tipo_fuente AS ENUM ('SUCURSAL', 'VENDEDOR', 'MIXTA');

CREATE TYPE estado_fichero AS ENUM ('PROCESADO', 'SIN_ASIGNAR', 'SIN_FECHA', 'ERROR');

-- De dónde salió la fecha del día. Un día fechado por el nombre del fichero no
-- vale lo mismo que uno fechado por los propios puntos, y el panel lo dice.
CREATE TYPE origen_fecha AS ENUM ('PUNTOS', 'NOMBRE', 'DRIVE', 'NINGUNO');

-- Por qué un punto no cuenta. Se marca, NO se borra: si mañana el umbral resulta
-- malo, se recalcula sin volver a bajar nada de Drive.
CREATE TYPE calidad_punto AS ENUM ('OK', 'SALTO', 'IMPRECISO', 'DUPLICADO', 'SIN_HORA');

CREATE TYPE estado_dia AS ENUM (
    'OK', 'SIN_FICHERO', 'SIN_FECHA', 'SIN_MOVIMIENTO', 'MOVIMIENTO_ESCASO', 'NO_LABORABLE'
);

-- ---------------------------------------------------------------------------
-- Personas y estructura
-- ---------------------------------------------------------------------------

-- Espejo local de la Organization de procovar-auth: aquí solo lo que hace falta
-- para consultar rápido; la identidad manda allá.
CREATE TABLE sucursal (
    id          TEXT PRIMARY KEY,
    auth_org_id TEXT UNIQUE,
    nombre      TEXT NOT NULL,
    activa      BOOLEAN NOT NULL DEFAULT TRUE,
    timezone    TEXT NOT NULL DEFAULT 'America/Havana',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- auth_user_id es NULL mientras sea alguien que solo aparece en los GPX y aún no
-- tiene cuenta: pasa al principio, cuando se están casando los alias.
CREATE TABLE trabajador (
    id           TEXT PRIMARY KEY,
    auth_user_id TEXT UNIQUE,
    nombre       TEXT NOT NULL,
    sucursal_id  TEXT NOT NULL REFERENCES sucursal (id),
    es_vendedor  BOOLEAN NOT NULL DEFAULT TRUE,
    activo       BOOLEAN NOT NULL DEFAULT TRUE,
    -- Alta y baja: quien entró en marzo no debe salir como ausente en enero.
    desde        DATE NOT NULL DEFAULT CURRENT_DATE,
    hasta        DATE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX trabajador_sucursal_idx ON trabajador (sucursal_id, activo);

-- Quién supervisa a quién, CON VIGENCIAS.
--
-- No es una columna supervisor_id en trabajador a propósito: el panel consulta
-- semanas pasadas y hay que poder responder "quién supervisaba a este vendedor
-- en agosto" aunque en octubre ya haya cambiado de equipo. Con una columna, el
-- histórico se falsea solo cada vez que alguien cambia de jefe.
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
-- Origen de los datos: carpetas de Drive
-- ---------------------------------------------------------------------------

-- Modelo heredado de la ingesta de PEDIDO: una cuenta padre de Google comparte
-- hasta 4 carpetas (límite de Google), así que credential_ref agrupa las
-- carpetas que se leen con la misma credencial.
CREATE TABLE drive_source (
    id             TEXT PRIMARY KEY,
    nombre         TEXT NOT NULL,
    folder_id      TEXT NOT NULL UNIQUE,
    tipo           tipo_fuente NOT NULL DEFAULT 'SUCURSAL',
    sucursal_id    TEXT REFERENCES sucursal (id),
    -- Si tipo = VENDEDOR, a quién pertenece todo lo que caiga aquí.
    trabajador_id  TEXT REFERENCES trabajador (id),
    credential_ref TEXT NOT NULL,
    activa         BOOLEAN NOT NULL DEFAULT TRUE,
    -- Cursor del barrido incremental: último modifiedTime visto. El repaso
    -- nocturno lo ignora a propósito y recorre la carpeta entera.
    cursor         TIMESTAMPTZ,
    ultimo_barrido TIMESTAMPTZ,
    ultimo_error   TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX drive_source_activa_idx ON drive_source (activa);

-- Registro de cada .gpx visto en Drive. Es el "ya procesé esto" del sistema: los
-- ficheros NO se mueven ni se borran del Drive (a diferencia de la ingesta de
-- PEDIDO), porque esas carpetas las ven los propios trabajadores.
CREATE TABLE gpx_file (
    id               TEXT PRIMARY KEY,
    source_id        TEXT NOT NULL REFERENCES drive_source (id),
    drive_file_id    TEXT NOT NULL UNIQUE,
    -- sha256 del contenido: si el mismo fichero aparece en dos carpetas se
    -- ingiere una sola vez; si se re-sube corregido, cambia el hash.
    sha256           TEXT NOT NULL UNIQUE,
    nombre           TEXT NOT NULL,
    ruta_carpeta     TEXT,
    tamano_bytes     INTEGER,
    drive_created_at TIMESTAMPTZ,
    estado           estado_fichero NOT NULL,
    error            TEXT,
    trabajador_id    TEXT REFERENCES trabajador (id),
    sucursal_id      TEXT REFERENCES sucursal (id),
    -- Día al que corresponde, en fecha local de la sucursal.
    fecha            DATE,
    origen_fecha     origen_fecha NOT NULL DEFAULT 'NINGUNO',
    puntos_total     INTEGER NOT NULL DEFAULT 0,
    puntos_validos   INTEGER NOT NULL DEFAULT 0,
    primer_fix       TIMESTAMPTZ,
    ultimo_fix       TIMESTAMPTZ,
    -- Texto crudo del que se intentó deducir el vendedor, para que el admin vea
    -- en la bandeja QUÉ tiene que casar.
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
    -- BIGSERIAL y no TEXT: es la tabla que crece de verdad (~141 k filas al día
    -- con 8 sucursales), y a partir del año conviene particionarla por mes.
    id            BIGSERIAL PRIMARY KEY,
    gpx_file_id   TEXT NOT NULL REFERENCES gpx_file (id) ON DELETE CASCADE,
    trabajador_id TEXT REFERENCES trabajador (id),
    sucursal_id   TEXT REFERENCES sucursal (id),
    -- Instante en UTC. El filtrado por jornada se hace con AT TIME ZONE.
    ts            TIMESTAMPTZ,
    lat           DOUBLE PRECISION NOT NULL,
    lon           DOUBLE PRECISION NOT NULL,
    ele           DOUBLE PRECISION,
    speed         DOUBLE PRECISION,
    accuracy      DOUBLE PRECISION,
    -- Orden dentro del fichero: mantiene la secuencia aunque falten las horas.
    seq           INTEGER NOT NULL,
    quality       calidad_punto NOT NULL DEFAULT 'OK'
);

CREATE INDEX track_point_trabajador_ts_idx ON track_point (trabajador_id, ts);
CREATE INDEX track_point_sucursal_ts_idx ON track_point (sucursal_id, ts);
CREATE INDEX track_point_fichero_idx ON track_point (gpx_file_id, seq);

-- Un vendedor, un día. Precalculado en la ingesta: el panel y el reporte no
-- pueden depender de escanear 18 000 puntos cada vez que alguien abre una
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
    -- Distancia máxima al centroide, en metros: la señal que decide la
    -- inmovilidad, porque es la única que el ruido del GPS no corrompe.
    radio_dispersion DOUBLE PRECISION,
    centroide_lat    DOUBLE PRECISION,
    centroide_lon    DOUBLE PRECISION,
    -- Dónde pasó el día quieto: el dato que de verdad se quiere al abrir el caso.
    lugar_texto      TEXT,
    banderas         TEXT[] NOT NULL DEFAULT '{}',
    -- NULL cuando el estado es SIN_FICHERO: no hay fichero al que apuntar.
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
    -- Cliente más cercano, si se activa el cruce con la geo de clientes.
    cliente_ref         TEXT,
    cliente_nombre      TEXT,
    distancia_cliente_m DOUBLE PRECISION,
    seq                 INTEGER NOT NULL
);

CREATE INDEX stop_trabajador_idx ON stop (trabajador_id, inicio);
CREATE INDEX stop_dia_idx ON stop (track_day_id, seq);

-- Alias de dispositivo → vendedor. Es lo que hace robusta la resolución sin
-- saber de antemano cómo se llaman los ficheros: el admin casa un alias una vez
-- desde la bandeja y no vuelve a tocarlo.
CREATE TABLE device_alias (
    id             TEXT PRIMARY KEY,
    -- Normalizado (minúsculas, sin tildes, sin separadores) para casar de verdad.
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

-- Umbrales por sucursal. Todo configurable a propósito: no es lo mismo un
-- vendedor de ciudad que uno que cubre tres municipios, y los valores buenos
-- solo se sabrán cuando haya datos reales.
CREATE TABLE sucursal_config (
    id                     TEXT PRIMARY KEY,
    sucursal_id            TEXT NOT NULL UNIQUE REFERENCES sucursal (id) ON DELETE CASCADE,
    jornada_inicio         TEXT NOT NULL DEFAULT '09:00',
    jornada_fin            TEXT NOT NULL DEFAULT '16:00',
    -- 1 = lunes … 7 = domingo. Por defecto, lunes a viernes.
    dias_laborables        INTEGER[] NOT NULL DEFAULT '{1,2,3,4,5}',
    parada_radio_m         INTEGER NOT NULL DEFAULT 60,
    parada_minutos         INTEGER NOT NULL DEFAULT 5,
    -- Suelo de ruido: los tramos más cortos no suman kilómetros.
    paso_minimo_m          INTEGER NOT NULL DEFAULT 25,
    -- Inmovilidad: radio de dispersión + jornada mínima para poder afirmarlo.
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

-- Sin esto, un 1 de mayo saldría como ausencia de toda la plantilla.
CREATE TABLE feriado (
    id          TEXT PRIMARY KEY,
    fecha       DATE NOT NULL,
    nombre      TEXT NOT NULL,
    -- NULL = feriado nacional, aplica a todas las sucursales.
    sucursal_id TEXT REFERENCES sucursal (id)
);

CREATE UNIQUE INDEX feriado_unico_idx
    ON feriado (fecha, COALESCE(sucursal_id, ''));

-- Auditoría de cada barrido: qué se leyó, qué entró y qué falló.
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
