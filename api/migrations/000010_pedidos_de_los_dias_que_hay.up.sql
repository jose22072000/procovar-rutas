-- Los pines se traen de los días QUE YA TENEMOS, no de una ventana fija.
--
-- La primera versión miraba tres semanas hacia atrás desde hoy y ya. Pero aquí hay
-- dos mil días de recorrido cargados desde el backfill, y de ninguno de ellos se
-- había pedido nunca su lista de clientes: se abría un día de agosto, salía la ruta,
-- y no había con qué compararla. La ventana no es el criterio correcto — el criterio
-- es «de este día tengo ruta, así que quiero saber por dónde debía pasar».
--
-- PEDIDO deja preguntar por fecha (`?desde=&hasta=`), así que se puede ir hacia atrás
-- todo lo que haga falta. Lo único que hay que llevar es la cuenta de qué días ya se
-- preguntaron, y para eso es esta tabla.

-- Un día del que ya se le preguntó a PEDIDO.
--
-- Hace falta una tabla y no vale con mirar si hay pedidos de ese día: un día en el
-- que NO hubo ningún pedido es indistinguible de uno que nunca se preguntó, y sin
-- esto se volvería a preguntar por él en cada pasada, para siempre.
CREATE TABLE dia_pedidos (
    fecha     DATE PRIMARY KEY,
    traido_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Cuántos pedidos tenía. Cero es una respuesta válida y se guarda como tal.
    pedidos   INTEGER NOT NULL DEFAULT 0,
    -- Si la última vez se pidió entero o sólo lo que había cambiado.
    completo  BOOLEAN NOT NULL DEFAULT FALSE
);

-- Los días ya traídos, para los que ya estaban antes de esta migración: si hay
-- pedidos de ese día, es que se preguntó por él.
INSERT INTO dia_pedidos (fecha, pedidos, completo)
SELECT fecha, count(*)::int, TRUE
FROM pedido
GROUP BY fecha
ON CONFLICT (fecha) DO NOTHING;
