-- El espejo de PEDIDO y el cruce de sus pedidos con el recorrido.
--
-- Nada de aquí escribe en PEDIDO: es su dato y allí se queda. Lo que se guarda es
-- una copia reconciliable —clave (sucursal, ref)— que se puede tirar y volver a
-- traer sincronizando.
--
-- Las columnas siguen en español porque la base no se renombra; los ALIAS de estas
-- consultas se escriben en inglés directamente, que es como sale el Go.

-- ---------------------------------------------------------------------------
-- Espejo
-- ---------------------------------------------------------------------------

-- name: UpsertClient :exec
INSERT INTO pedido_cliente (
    id, sucursal_id, ref, codigo, nombre, direccion, municipio, zona, lat, lon, actualizado_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (sucursal_id, ref) DO UPDATE SET
    codigo = EXCLUDED.codigo,
    nombre = EXCLUDED.nombre,
    direccion = EXCLUDED.direccion,
    municipio = EXCLUDED.municipio,
    zona = EXCLUDED.zona,
    lat = EXCLUDED.lat,
    lon = EXCLUDED.lon,
    actualizado_at = now();

-- El pedido entra SIN trabajador: emparejar el vendedor es un paso aparte
-- (LinkOrdersToSellers) que se vuelve a correr cuando alguien arregla un
-- emparejamiento, sin tener que sincronizar de nuevo.
-- name: UpsertOrder :exec
INSERT INTO pedido (
    id, sucursal_id, ref, folio, fecha, cliente_id,
    vendedor_ref, vendedor_codigo, vendedor_nombre, estado, requiere_domicilio,
    origen_actualizado_at, actualizado_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
ON CONFLICT (sucursal_id, ref) DO UPDATE SET
    folio = EXCLUDED.folio,
    fecha = EXCLUDED.fecha,
    cliente_id = EXCLUDED.cliente_id,
    vendedor_ref = EXCLUDED.vendedor_ref,
    vendedor_codigo = EXCLUDED.vendedor_codigo,
    vendedor_nombre = EXCLUDED.vendedor_nombre,
    estado = EXCLUDED.estado,
    requiere_domicilio = EXCLUDED.requiere_domicilio,
    origen_actualizado_at = EXCLUDED.origen_actualizado_at,
    actualizado_at = now();

-- Por dónde se quedó la última sincronización, sacado del propio espejo.
--
-- El cursor NO se guarda aparte a propósito: un cursor en su tabla se desincroniza
-- del dato el día que algo se borre a mano o falle a medias, y entonces deja de
-- traerse lo que falta sin que nadie se entere. Preguntándoselo a las filas que hay,
-- el cursor no puede mentir.
-- name: LastOrderCursor :one
-- Con el espejo vacío no hay cursor y sale `epoch`, que es lo que se quiere en la
-- primera pasada: traerlo todo. (`coalesce` y no un nulo porque una fecha nula leída
-- como no nula revienta al escanear la fila, y sqlc no acierta a inferirlo aquí.)
SELECT coalesce(max(origen_actualizado_at), 'epoch'::timestamptz)::timestamptz AS cursor
FROM pedido;

-- Los pines que YA están guardados, para no volver a escribir los que no se han
-- movido. Con ocho mil clientes por sucursal, la pasada horaria reescribía ocho mil
-- filas idénticas para acabar dejándolo todo como estaba.
-- name: ClientPins :many
SELECT ref, nombre, lat, lon FROM pedido_cliente WHERE sucursal_id = $1;

-- Los pedidos viejos que ya no vienen en la ventana sincronizada NO se borran: el
-- calendario mira semanas pasadas y borrarlos dejaría esas semanas en blanco. Solo
-- se limpia lo que quedó huérfano de cliente.
-- name: DeleteOrdersOfDayNotIn :execrows
DELETE FROM pedido
WHERE sucursal_id = @branch_id
  AND fecha BETWEEN @from_date::date AND @to_date::date
  AND NOT (ref = ANY (@refs::text[]));

-- name: SetBranchCode :exec
UPDATE sucursal SET codigo = @code, updated_at = now()
WHERE id = @id AND (codigo IS NULL OR codigo <> @code);

-- ---------------------------------------------------------------------------
-- Quién es quién: vendedor de PEDIDO ↔ trabajador de Rutas
-- ---------------------------------------------------------------------------

-- name: SellersForMatch :many
SELECT t.id, t.nombre, t.sucursal_id
FROM trabajador t
WHERE t.activo AND t.es_vendedor;

-- Un emparejamiento MANUAL no lo pisa el automático: si una persona ya dijo quién
-- es, el algoritmo no vuelve a opinar.
-- name: UpsertSellerLink :exec
INSERT INTO vendedor_pedido (
    id, sucursal_id, trabajador_id, vendedor_codigo, vendedor_nombre, origen
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (sucursal_id, vendedor_codigo) DO UPDATE SET
    trabajador_id = CASE
        WHEN vendedor_pedido.origen = 'manual' AND EXCLUDED.origen = 'auto'
        THEN vendedor_pedido.trabajador_id ELSE EXCLUDED.trabajador_id END,
    vendedor_nombre = EXCLUDED.vendedor_nombre,
    origen = CASE
        WHEN vendedor_pedido.origen = 'manual' AND EXCLUDED.origen = 'auto'
        THEN 'manual' ELSE EXCLUDED.origen END;

-- name: LinkOrdersToSellers :execrows
UPDATE pedido p
SET trabajador_id = v.trabajador_id
FROM vendedor_pedido v
WHERE v.sucursal_id = p.sucursal_id
  AND v.vendedor_codigo = p.vendedor_codigo
  AND p.trabajador_id IS DISTINCT FROM v.trabajador_id;

-- Los vendedores de PEDIDO que no se pudieron emparejar con nadie de aquí. Es
-- información, no un error: mientras estén así, sus pedidos no se cruzan con
-- ninguna ruta y el panel tiene que decirlo en vez de enseñar un cero.
-- name: UnlinkedVendors :many
SELECT
    p.sucursal_id,
    coalesce(s.nombre, '')      AS branch,
    p.vendedor_codigo,
    max(p.vendedor_nombre)::text AS vendor_label,
    count(*)::int               AS orders,
    max(p.fecha)::date          AS last_order
FROM pedido p
LEFT JOIN sucursal s ON s.id = p.sucursal_id
WHERE p.trabajador_id IS NULL
  AND (@branch_id::text = '' OR p.sucursal_id = @branch_id)
GROUP BY p.sucursal_id, s.nombre, p.vendedor_codigo
ORDER BY count(*) DESC, max(p.vendedor_nombre);

-- name: SellerLinks :many
SELECT v.*, t.nombre AS seller
FROM vendedor_pedido v
JOIN trabajador t ON t.id = v.trabajador_id
WHERE (@branch_id::text = '' OR v.sucursal_id = @branch_id)
ORDER BY v.vendedor_nombre;

-- ---------------------------------------------------------------------------
-- El cruce
-- ---------------------------------------------------------------------------

-- El día tiene que EXISTIR para poder colgarle las visitas, y hoy a media mañana
-- todavía no existe: la fila la crea el fichero cuando el vendedor termina la
-- jornada, o el repaso nocturno al día siguiente. Sin esto, «hoy tiene 8 pedidos»
-- no se podría enseñar hasta mañana, que es justo cuando ya no sirve.
--
-- Solo para (vendedor, día) que TIENEN pedido: eso ya implica que era día de
-- trabajo. Crear la fila para todos los vendedores y todos los días del rango
-- pintaría de rojo los sábados.
-- name: EnsureDaysForOrders :execrows
INSERT INTO track_day (id, trabajador_id, sucursal_id, fecha, estado)
SELECT DISTINCT
    md5(p.trabajador_id || ':' || to_char(p.fecha, 'YYYY-MM-DD')),
    p.trabajador_id,
    p.sucursal_id,
    p.fecha,
    -- El literal se marca como enum a mano: con el DISTINCT de arriba, Postgres le
    -- resuelve el tipo ANTES de mirar la columna y lo manda como `text`, que la
    -- tabla rechaza. sqlc lo daba por bueno; solo se ve al ejecutarlo.
    'SIN_FICHERO'::estado_dia
FROM pedido p
WHERE p.trabajador_id IS NOT NULL
  AND p.fecha BETWEEN @from_date::date AND @to_date::date
ON CONFLICT (trabajador_id, fecha) DO NOTHING;

-- Los días que hay que volver a cruzar: los que tienen pedidos en el rango. Un día
-- sin pedidos no se toca — no hay nada contra qué contrastar el recorrido.
-- name: DaysToCross :many
SELECT d.id, d.trabajador_id, d.sucursal_id, d.fecha
FROM track_day d
WHERE d.fecha BETWEEN @from_date::date AND @to_date::date
  AND EXISTS (
      SELECT 1 FROM pedido p
      WHERE p.trabajador_id = d.trabajador_id AND p.fecha = d.fecha
  )
ORDER BY d.fecha, d.trabajador_id;

-- name: OrdersForDay :many
SELECT
    p.id                   AS order_id,
    p.folio,
    p.estado               AS order_status,
    c.id                   AS client_id,
    c.nombre               AS client_name,
    c.lat,
    c.lon
FROM pedido p
JOIN pedido_cliente c ON c.id = p.cliente_id
WHERE p.trabajador_id = @seller_id::text AND p.fecha = @date::date
ORDER BY c.nombre;

-- name: StopsForCross :many
SELECT id, inicio, fin, duracion_min, lat, lon
FROM stop
WHERE track_day_id = $1
ORDER BY seq;

-- name: DeleteDayVisits :exec
DELETE FROM visita WHERE track_day_id = $1;

-- name: CreateVisit :exec
INSERT INTO visita (
    id, track_day_id, pedido_id, cliente_id, stop_id, visitado, distancia_m, hora, minutos, calculado_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now())
ON CONFLICT (track_day_id, pedido_id) DO UPDATE SET
    cliente_id = EXCLUDED.cliente_id,
    stop_id = EXCLUDED.stop_id,
    visitado = EXCLUDED.visitado,
    distancia_m = EXCLUDED.distancia_m,
    hora = EXCLUDED.hora,
    minutos = EXCLUDED.minutos,
    calculado_at = now();

-- La parada deja de ser un punto anónimo y pasa a llamarse como el cliente al que
-- se le atribuye. Se limpia antes de cada cruce: si un pedido se anula, su parada
-- no puede seguir llevando su nombre.
-- name: ClearDayStopClients :exec
UPDATE stop SET cliente_ref = NULL, cliente_nombre = NULL, distancia_cliente_m = NULL
WHERE track_day_id = $1;

-- name: SetStopClient :exec
UPDATE stop SET cliente_ref = $2, cliente_nombre = $3, distancia_cliente_m = $4
WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Lo que lee el panel
-- ---------------------------------------------------------------------------

-- Por vendedor y día: cuántos pedidos tenía y a cuántos se acercó. Es la columna
-- que convierte "hizo 30 km" en "hizo la ruta".
-- name: VisitSummary :many
SELECT
    d.trabajador_id,
    d.fecha,
    count(*)::int                             AS orders,
    count(*) FILTER (WHERE v.visitado)::int   AS visited
FROM visita v
JOIN track_day d ON d.id = v.track_day_id
WHERE d.fecha BETWEEN @from_date::date AND @to_date::date
  AND (@branch_id::text = '' OR d.sucursal_id = @branch_id)
  AND (cardinality(@sellers::text[]) = 0 OR d.trabajador_id = ANY (@sellers))
  AND (@exclude::text = '' OR d.trabajador_id <> @exclude)
GROUP BY d.trabajador_id, d.fecha;

-- Los clientes del día con su veredicto, para dibujarlos como una capa del mapa.
-- name: DayVisits :many
SELECT
    v.id,
    v.visitado,
    v.distancia_m,
    v.hora,
    v.minutos,
    v.stop_id,
    p.folio,
    p.estado    AS order_status,
    c.id        AS client_id,
    c.codigo    AS client_code,
    c.nombre    AS client_name,
    c.direccion AS client_address,
    c.municipio AS client_municipality,
    c.lat,
    c.lon
FROM visita v
JOIN pedido p ON p.id = v.pedido_id
JOIN pedido_cliente c ON c.id = v.cliente_id
WHERE v.track_day_id = $1
ORDER BY v.visitado DESC, v.hora NULLS LAST, c.nombre;

-- Toda la cartera del vendedor en la sucursal, visitada o no. Es la otra capa del
-- mapa: dónde están TODOS sus clientes, no solo los que tenían pedido ese día.
-- name: BranchClients :many
SELECT c.id, c.codigo, c.nombre, c.direccion, c.municipio, c.lat, c.lon
FROM pedido_cliente c
WHERE c.sucursal_id = $1
ORDER BY c.nombre;

-- ---------------------------------------------------------------------------
-- Bitácora
-- ---------------------------------------------------------------------------

-- name: OpenOrderSync :one
INSERT INTO pedido_sync (id, tipo) VALUES ($1, $2) RETURNING *;

-- name: CloseOrderSync :exec
UPDATE pedido_sync
SET fin = now(), clientes = $2, pedidos = $3, cruces = $4, ok = $5, detalle = $6
WHERE id = $1;

-- name: RecentOrderSyncs :many
SELECT * FROM pedido_sync ORDER BY inicio DESC LIMIT $1;
