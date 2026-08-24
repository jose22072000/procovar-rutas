-- Los pedidos del día, para poder contrastarlos con el recorrido.
--
-- La pregunta que este panel no sabía contestar todavía era la única que de verdad
-- importa: el vendedor se movió 30 km, ¿pero pasó por sus clientes? Los kilómetros
-- por sí solos no dicen nada — se hacen igual dando vueltas.
--
-- PEDIDO es el DUEÑO de los clientes y de los pedidos, y aquí no se le discute eso:
-- lo de abajo es un ESPEJO de solo lectura, refrescado por HTTP contra su API de
-- integración. Nada de esto se edita desde Rutas, y si una fila se pierde se vuelve
-- a traer sincronizando. Por eso no hay `created_by` ni auditoría: no es un dato de
-- aquí.
--
-- Se espeja en vez de consultarse al vuelo porque el calendario pinta 53 vendedores
-- × 5 días de una vez: preguntarle a PEDIDO por cada celda son 265 llamadas para
-- dibujar una pantalla, y el día que PEDIDO esté caído el calendario dejaría de
-- verse entero en lugar de quedarse sin una columna.

-- El código de sucursal de PEDIDO (CAM, STG, HAB…). En Rutas las sucursales nacen
-- del nombre de la cuenta de Drive y se identifican por `clave`; el código es lo que
-- las ata a la otra aplicación, y se rellena solo con lo que traen los pedidos.
ALTER TABLE sucursal ADD COLUMN IF NOT EXISTS codigo TEXT;

-- ---------------------------------------------------------------------------
-- El espejo de PEDIDO
-- ---------------------------------------------------------------------------

-- Un cliente con coordenadas. SIN coordenadas no se espeja: el único uso que tiene
-- aquí es medir si el vendedor pasó por su puerta, y sin lat/lon eso no se puede
-- ni intentar.
CREATE TABLE pedido_cliente (
    id             TEXT PRIMARY KEY,
    sucursal_id    TEXT NOT NULL REFERENCES sucursal (id),
    -- Su identificador EN PEDIDO. Es la clave con la que se reconcilia en cada
    -- sincronización, no el nombre: los nombres se corrigen y se reescriben.
    ref            TEXT NOT NULL,
    codigo         TEXT,
    nombre         TEXT NOT NULL,
    direccion      TEXT,
    municipio      TEXT,
    zona           TEXT,
    lat            DOUBLE PRECISION NOT NULL,
    lon            DOUBLE PRECISION NOT NULL,
    actualizado_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pedido_cliente_unico UNIQUE (sucursal_id, ref)
);

CREATE INDEX pedido_cliente_sucursal_idx ON pedido_cliente (sucursal_id);

-- Un pedido de un día. Lo que se conserva es lo justo para el cruce: de quién es,
-- de qué día, y a qué cliente iba.
CREATE TABLE pedido (
    id                 TEXT PRIMARY KEY,
    sucursal_id        TEXT NOT NULL REFERENCES sucursal (id),
    ref                TEXT NOT NULL,
    folio              TEXT,
    fecha              DATE NOT NULL,
    cliente_id         TEXT REFERENCES pedido_cliente (id) ON DELETE SET NULL,
    -- El vendedor TAL COMO LO ESCRIBE PEDIDO, y aparte el trabajador de aquí al que
    -- se emparejó. Se guardan los dos: si el emparejamiento estaba mal, el original
    -- sigue estando para rehacerlo sin volver a sincronizar.
    vendedor_ref       TEXT,
    vendedor_codigo    TEXT,
    vendedor_nombre    TEXT,
    trabajador_id      TEXT REFERENCES trabajador (id) ON DELETE SET NULL,
    estado             TEXT,
    requiere_domicilio BOOLEAN NOT NULL DEFAULT FALSE,
    actualizado_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pedido_unico UNIQUE (sucursal_id, ref)
);

CREATE INDEX pedido_dia_idx ON pedido (trabajador_id, fecha);
CREATE INDEX pedido_sucursal_fecha_idx ON pedido (sucursal_id, fecha);

-- ---------------------------------------------------------------------------
-- Emparejar los dos mundos: quién es quién
-- ---------------------------------------------------------------------------

-- El vendedor de PEDIDO ("andy.almanza") y el trabajador de Rutas ("ANDY"), que
-- nacen de sitios distintos: uno del maestro de vendedores, el otro del nombre de
-- una carpeta de Drive. Se intenta emparejar solo por el nombre normalizado, y lo
-- que no case se empareja a mano UNA vez.
--
-- Es tabla y no una columna en `trabajador` por lo mismo que `device_alias`: un
-- trabajador puede arrastrar más de un código a lo largo del tiempo, y la fila
-- guarda de dónde salió el emparejamiento.
CREATE TABLE vendedor_pedido (
    id              TEXT PRIMARY KEY,
    sucursal_id     TEXT NOT NULL REFERENCES sucursal (id),
    trabajador_id   TEXT NOT NULL REFERENCES trabajador (id) ON DELETE CASCADE,
    vendedor_codigo TEXT NOT NULL,
    vendedor_nombre TEXT NOT NULL,
    -- auto = lo emparejó el nombre; manual = lo dijo una persona y no se toca.
    origen          TEXT NOT NULL DEFAULT 'auto',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT vendedor_pedido_unico UNIQUE (sucursal_id, vendedor_codigo)
);

CREATE INDEX vendedor_pedido_trabajador_idx ON vendedor_pedido (trabajador_id);

-- ---------------------------------------------------------------------------
-- El cruce
-- ---------------------------------------------------------------------------

-- Un pedido del día contra el recorrido de ese día: ¿se acercó a la puerta del
-- cliente o no?
--
-- Se guarda calculado y no se resuelve al pintar porque el cálculo necesita las
-- paradas del día y la geo del cliente, y una pantalla que abre cinco días de
-- cincuenta vendedores no puede hacer ese cruce cincuenta veces por columna.
--
-- `visitado` se decide contra `sucursal_config.visita_radio_m` (80 m por defecto):
-- el GPS de un teléfono en la calle no cae en el portal exacto, y en un pueblo dos
-- clientes de la misma cuadra están a menos de eso. Por eso se guarda TAMBIÉN la
-- distancia: el que mira decide si 74 m le valen.
CREATE TABLE visita (
    id           TEXT PRIMARY KEY,
    track_day_id TEXT NOT NULL REFERENCES track_day (id) ON DELETE CASCADE,
    pedido_id    TEXT NOT NULL REFERENCES pedido (id) ON DELETE CASCADE,
    cliente_id   TEXT NOT NULL REFERENCES pedido_cliente (id) ON DELETE CASCADE,
    -- La parada que se le atribuye. NULL cuando no se acercó a nadie.
    stop_id      TEXT REFERENCES stop (id) ON DELETE SET NULL,
    visitado     BOOLEAN NOT NULL,
    -- A cuánto pasó de la puerta, aunque no cuente como visita. Es el dato que
    -- distingue "pasó de largo a 200 m" de "no se acercó en todo el día".
    distancia_m  DOUBLE PRECISION,
    hora         TIMESTAMPTZ,
    minutos      INTEGER,
    calculado_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT visita_unica UNIQUE (track_day_id, pedido_id)
);

CREATE INDEX visita_dia_idx ON visita (track_day_id);

-- ---------------------------------------------------------------------------
-- Bitácora de la sincronización
-- ---------------------------------------------------------------------------

-- Lo mismo que `import_log` hace con Drive: si mañana el calendario sale sin
-- pedidos, esto dice si fue que PEDIDO no contestó o que no había ninguno.
CREATE TABLE pedido_sync (
    id       TEXT PRIMARY KEY,
    tipo     TEXT NOT NULL,
    inicio   TIMESTAMPTZ NOT NULL DEFAULT now(),
    fin      TIMESTAMPTZ,
    clientes INTEGER NOT NULL DEFAULT 0,
    pedidos  INTEGER NOT NULL DEFAULT 0,
    cruces   INTEGER NOT NULL DEFAULT 0,
    ok       BOOLEAN NOT NULL DEFAULT FALSE,
    detalle  TEXT
);

CREATE INDEX pedido_sync_inicio_idx ON pedido_sync (inicio DESC);
