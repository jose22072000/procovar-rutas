-- El maestro de vendedores de PEDIDO, espejado.
--
-- Hasta ahora la lista de «quién falta por emparejar» se deducía de los PEDIDOS: un
-- vendedor solo aparecía si alguno de sus pedidos ya se había traído. Con ciento
-- cincuenta días de histórico todavía en la cola, eso daba una pantalla que se
-- contradecía a sí misma: veintidós vendedores marcados «sin emparejar» arriba y, un
-- palmo más abajo, «no falta ninguno por emparejar».
--
-- No era un fallo de cuentas: son dos preguntas distintas. Arriba, «¿este trabajador
-- tiene dueño en PEDIDO?». Abajo, «¿hay pedidos huérfanos?». Con el maestro entero
-- espejado las dos miran la misma lista, y quien empareja ve a TODOS desde el primer
-- momento — también al que aún no ha vendido nada.
CREATE TABLE pedido_vendedor (
    id             TEXT PRIMARY KEY,
    sucursal_id    TEXT REFERENCES sucursal (id),
    -- Su identificador en PEDIDO.
    ref            TEXT NOT NULL UNIQUE,
    -- El código del maestro ("andy.almanza"), único global allí.
    codigo         TEXT,
    nombre         TEXT NOT NULL,
    activo         BOOLEAN NOT NULL DEFAULT TRUE,
    -- Cuántos pedidos lleva en PEDIDO. Dice si emparejarlo importa mucho o poco.
    pedidos        INTEGER NOT NULL DEFAULT 0,
    actualizado_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX pedido_vendedor_sucursal_idx ON pedido_vendedor (sucursal_id, activo);
CREATE INDEX pedido_vendedor_codigo_idx ON pedido_vendedor (codigo);
