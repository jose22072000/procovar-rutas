package pedido

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/store"
	"github.com/procovar/procovar-rutas/api/internal/testdb"
)

// El cruce de verdad, contra Postgres: tres pedidos, dos paradas, y el veredicto.
//
// Se prueba con base de datos y no con una función pura porque lo que puede salir
// mal aquí no es la fórmula del haversine —esa ya está probada en metrics— sino el
// paso siguiente: qué se guarda, qué se borra al recalcular, y a qué parada se le
// atribuye cada cliente.

func servicio(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.Open(t)
	// Sin cliente HTTP: cruzar no habla con PEDIDO, solo mide lo que ya está aquí.
	// Es justo lo que permite volver a cruzar cuando alguien arregla un
	// emparejamiento, sin bajarse un mes de pedidos otra vez.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Sin cola y sin pausa: aquí se prueba el cruce, no el ritmo al que se le pide a
	// PEDIDO. La pausa haría que cada prueba tardara cinco segundos por día.
	return NewService(pool, nil, nil, nil, log, 21, time.Nanosecond), pool
}

// Un grado de latitud son ~111 km, así que 0,00009° ≈ 10 m.
const gradoPorMetro = 1.0 / 111_320.0

func TestCruceDecideQuienFueVisitado(t *testing.T) {
	svc, pool := servicio(t)
	q := store.New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE visita, vendedor_pedido, pedido, pedido_cliente,
		stop, track_day, track_point, gpx_file, drive_source, trabajador, sucursal CASCADE`); err != nil {
		t.Fatal(err)
	}

	fecha, _ := time.Parse("2006-01-02", "2026-08-12")
	const lat, lon = 21.38, -77.91

	sembrar(t, pool, fecha)

	// Dos paradas, separadas de sobra para que no se confundan entre ellas.
	crearParada(t, pool, "p1", lat, lon, 20)
	crearParada(t, pool, "p2", lat+500*gradoPorMetro, lon, 8)

	// Tres clientes con pedido ese día:
	//   c1, a 15 m de la primera parada  → visitado
	//   c2, a 25 m de la segunda parada  → visitado
	//   c3, a 400 m de todo              → NO visitado, pero se guarda a cuánto pasó
	crearPedido(t, q, ctx, "c1", "o1", "Bodega La Esquina", lat+15*gradoPorMetro, lon)
	crearPedido(t, q, ctx, "c2", "o2", "Cafetería El Puente", lat+525*gradoPorMetro, lon)
	crearPedido(t, q, ctx, "c3", "o3", "Kiosko Lejano", lat+1500*gradoPorMetro, lon)

	if err := svc.CrossDay(ctx, "d1", "t1", fecha, 80); err != nil {
		t.Fatal(err)
	}

	visitas, err := q.DayVisits(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(visitas) != 3 {
		t.Fatalf("los tres pedidos del día tienen que tener veredicto, hay %d", len(visitas))
	}

	porCliente := map[string]store.DayVisitsRow{}
	for _, v := range visitas {
		porCliente[v.ClientName] = v
	}

	if !porCliente["Bodega La Esquina"].Visited {
		t.Error("el cliente a 15 m de una parada cuenta como visitado")
	}
	if !porCliente["Cafetería El Puente"].Visited {
		t.Error("el cliente a 25 m de la segunda parada cuenta como visitado")
	}

	lejano := porCliente["Kiosko Lejano"]
	if lejano.Visited {
		t.Error("el cliente a 400 m no se visitó")
	}
	// Y lo importante del no visitado: a cuánto pasó. «Pasó de largo a 400 m» no es
	// lo mismo que «no se acercó en todo el día», y sin este número no se distinguen.
	if lejano.DistanceM == nil {
		t.Fatal("aunque no se visitara, hay que guardar a cuánto pasó")
	}
	if *lejano.DistanceM < 900 || *lejano.DistanceM > 1100 {
		t.Errorf("pasó a ~1000 m de él, y se guardó %.0f", *lejano.DistanceM)
	}

	// Cada parada deja de ser un punto anónimo y se queda con SU cliente, no con el
	// mismo para las dos.
	paradas, err := q.DayStops(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	nombres := map[string]string{}
	for _, p := range paradas {
		if p.ClientName != nil {
			nombres[p.ID] = *p.ClientName
		}
	}
	if nombres["p1"] != "Bodega La Esquina" {
		t.Errorf("la primera parada es la de la bodega, y quedó como %q", nombres["p1"])
	}
	if nombres["p2"] != "Cafetería El Puente" {
		t.Errorf("la segunda parada es la de la cafetería, y quedó como %q", nombres["p2"])
	}
}

// Volver a cruzar tiene que dejar el día como si fuera la primera vez: si un pedido
// se anula, su visita no puede seguir contando, y su nombre no puede seguir pegado a
// una parada.
func TestVolverACruzarNoDejaRestos(t *testing.T) {
	svc, pool := servicio(t)
	q := store.New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE visita, vendedor_pedido, pedido, pedido_cliente,
		stop, track_day, track_point, gpx_file, drive_source, trabajador, sucursal CASCADE`); err != nil {
		t.Fatal(err)
	}

	fecha, _ := time.Parse("2006-01-02", "2026-08-12")
	const lat, lon = 21.38, -77.91

	sembrar(t, pool, fecha)
	crearParada(t, pool, "p1", lat, lon, 20)
	crearPedido(t, q, ctx, "c1", "o1", "Bodega La Esquina", lat+15*gradoPorMetro, lon)

	if err := svc.CrossDay(ctx, "d1", "t1", fecha, 80); err != nil {
		t.Fatal(err)
	}

	// Se anula el pedido y se vuelve a cruzar.
	if _, err := pool.Exec(ctx, `DELETE FROM pedido WHERE id = 'o1'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.CrossDay(ctx, "d1", "t1", fecha, 80); err != nil {
		t.Fatal(err)
	}

	visitas, err := q.DayVisits(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(visitas) != 0 {
		t.Errorf("el pedido anulado no puede seguir contando: quedan %d visitas", len(visitas))
	}

	paradas, err := q.DayStops(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(paradas) != 1 {
		t.Fatalf("la parada sigue siendo una: %d", len(paradas))
	}
	if paradas[0].ClientName != nil {
		t.Errorf("la parada no puede seguir llamándose %q", *paradas[0].ClientName)
	}
}

// Un pedido de HOY, antes de que el vendedor suba nada: no hay fila de día, así que
// no habría dónde colgar la visita y los pedidos de hoy no se verían hasta mañana.
// El cruce crea la fila —en SIN_FICHERO, que es lo que es— y así el calendario puede
// decir «hoy tiene 8 pedidos y no ha pasado por ninguno todavía».
func TestUnDiaSinFicheroTambienSeCruza(t *testing.T) {
	svc, pool := servicio(t)
	q := store.New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE visita, vendedor_pedido, pedido, pedido_cliente,
		stop, track_day, track_point, gpx_file, drive_source, trabajador, sucursal CASCADE`); err != nil {
		t.Fatal(err)
	}

	fecha, _ := time.Parse("2006-01-02", "2026-08-12")
	sembrar(t, pool, fecha)
	// La sembradora crea el día; aquí se quita a propósito, que es el caso real.
	if _, err := pool.Exec(ctx, `DELETE FROM track_day WHERE id = 'd1'`); err != nil {
		t.Fatal(err)
	}
	crearPedido(t, q, ctx, "c1", "o1", "Bodega La Esquina", 21.38, -77.91)

	n, err := svc.CrossRange(ctx, fecha, fecha)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("se debería haber cruzado 1 día, se cruzaron %d", n)
	}

	var id string
	if err := pool.QueryRow(ctx,
		`SELECT id FROM track_day WHERE trabajador_id = 't1' AND fecha = $1`, fecha).Scan(&id); err != nil {
		t.Fatalf("el día tendría que existir ya: %v", err)
	}

	visitas, err := q.DayVisits(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(visitas) != 1 || visitas[0].Visited {
		t.Fatalf("sin recorrido, el pedido está sin visitar: %+v", visitas)
	}
}

// El pin que ya está guardado no se vuelve a escribir.
//
// Es lo que hace que la pasada horaria no sea inútil: un cliente no se mueve, su pin
// es el mismo hoy que hace seis meses, y reescribir ocho mil filas idénticas cada
// hora es trabajo puro para dejarlo todo como estaba.
func TestElPinGuardadoNoSeReescribe(t *testing.T) {
	_, pool := servicio(t)
	q := store.New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE visita, vendedor_pedido, pedido, pedido_cliente,
		stop, track_day, track_point, gpx_file, drive_source, trabajador, sucursal CASCADE`); err != nil {
		t.Fatal(err)
	}
	fecha, _ := time.Parse("2006-01-02", "2026-08-12")
	sembrar(t, pool, fecha)
	crearPedido(t, q, ctx, "c1", "o1", "Bodega La Esquina", 21.38, -77.91)

	pines, err := q.ClientPins(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pines) != 1 {
		t.Fatalf("debería haber un pin guardado, hay %d", len(pines))
	}
	guardado := &pines[0]

	lat, lon := 21.38, -77.91
	mismo := &Client_{Name: "Bodega La Esquina", Lat: &lat, Lon: &lon}
	if pinCambio(guardado, mismo) {
		t.Error("el mismo cliente en el mismo sitio no hay que volver a escribirlo")
	}

	// Un temblor de redondeo al ir y venir en JSON tampoco es un movimiento.
	casi := lat + 1e-9
	if pinCambio(guardado, &Client_{Name: "Bodega La Esquina", Lat: &casi, Lon: &lon}) {
		t.Error("un centímetro de redondeo no es que el cliente se haya mudado")
	}

	// Mudarse sí.
	otro := lat + 0.001 // ~110 m
	if !pinCambio(guardado, &Client_{Name: "Bodega La Esquina", Lat: &otro, Lon: &lon}) {
		t.Error("si el cliente se mudó, hay que guardar el pin nuevo")
	}

	// Y que le corrijan el nombre, también: es lo que se lee en la lista y en el mapa.
	if !pinCambio(guardado, &Client_{Name: "Bodega La Esquina S.A.", Lat: &lat, Lon: &lon}) {
		t.Error("un nombre corregido hay que guardarlo")
	}

	// Y el que no está, es nuevo.
	if !pinCambio(nil, mismo) {
		t.Error("un cliente que no está guardado hay que guardarlo")
	}
}

// El cursor sale del propio espejo: sin nada guardado no hay cursor, y con pedidos
// guardados es el `updatedAt` más reciente de PEDIDO — no el nuestro.
func TestElCursorSaleDelEspejo(t *testing.T) {
	_, pool := servicio(t)
	q := store.New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE visita, vendedor_pedido, pedido, pedido_cliente,
		stop, track_day, track_point, gpx_file, drive_source, trabajador, sucursal CASCADE`); err != nil {
		t.Fatal(err)
	}

	// Con el espejo vacío: epoch, que es «tráelo todo».
	c, err := q.LastOrderCursor(ctx)
	if err != nil {
		t.Fatalf("con el espejo vacío el cursor tiene que poder leerse: %v", err)
	}
	if c.Year() > 1971 {
		t.Errorf("sin pedidos no hay cursor, y salió %s", c)
	}

	fecha, _ := time.Parse("2006-01-02", "2026-08-12")
	sembrar(t, pool, fecha)
	crearPedido(t, q, ctx, "c1", "o1", "Bodega La Esquina", 21.38, -77.91)

	// El pedido que sembramos no trae `updatedAt`; se le pone uno a mano, que es lo
	// que hará PEDIDO.
	movido := time.Date(2026, 8, 12, 15, 30, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx,
		`UPDATE pedido SET origen_actualizado_at = $1 WHERE id = 'o1'`, movido); err != nil {
		t.Fatal(err)
	}

	c, err = q.LastOrderCursor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Equal(movido) {
		t.Errorf("el cursor es el reloj de PEDIDO (%s), y salió %s", movido, c)
	}
}

// --- semilla ----------------------------------------------------------------

func sembrar(t *testing.T, pool *pgxpool.Pool, fecha time.Time) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO sucursal (id, nombre, clave, codigo) VALUES ('s1','Camagüey','camaguey','CAM')`)
	exec(`INSERT INTO trabajador (id, nombre, sucursal_id, desde) VALUES ('t1','ALEXANDER','s1','2020-01-01')`)
	exec(`INSERT INTO track_day (id, trabajador_id, sucursal_id, fecha, estado) VALUES ('d1','t1','s1',$1,'OK')`, fecha)
	exec(`INSERT INTO vendedor_pedido (id, sucursal_id, trabajador_id, vendedor_codigo, vendedor_nombre, origen)
	      VALUES ('v1','s1','t1','alexander.rodriguez','ALEXANDER RODRÍGUEZ','auto')`)
}

func crearParada(t *testing.T, pool *pgxpool.Pool, id string, lat, lon float64, minutos int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO stop (id, track_day_id, trabajador_id, sucursal_id, inicio, fin, duracion_min, lat, lon, seq)
		 VALUES ($1,'d1','t1','s1', now(), now(), $2, $3, $4, 0)`,
		id, minutos, lat, lon); err != nil {
		t.Fatal(err)
	}
}

func crearPedido(t *testing.T, q *store.Queries, ctx context.Context, clienteID, pedidoID, nombre string, lat, lon float64) {
	t.Helper()
	if err := q.UpsertClient(ctx, store.UpsertClientParams{
		ID: clienteID, BranchID: "s1", Ref: clienteID, Name: nombre, Lat: lat, Lon: lon,
	}); err != nil {
		t.Fatal(err)
	}
	codigo, vendedor := "alexander.rodriguez", "ALEXANDER RODRÍGUEZ"
	fecha, _ := time.Parse("2006-01-02", "2026-08-12")
	ref := clienteID
	if err := q.UpsertOrder(ctx, store.UpsertOrderParams{
		ID: pedidoID, BranchID: "s1", Ref: pedidoID, Date: fecha, ClientID: &ref,
		VendorCode: &codigo, VendorName: &vendedor,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.LinkOrdersToSellers(ctx); err != nil {
		t.Fatal(err)
	}
}
