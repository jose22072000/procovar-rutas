package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/procovar/procovar-rutas/api/internal/store"
	"github.com/procovar/procovar-rutas/api/internal/testdb"
)

// Igual que queries_test.go, y por la misma razón: estas consultas se EJECUTAN
// contra un Postgres de verdad con los parámetros que manda el panel.
//
// sqlc valida la sintaxis contra el esquema y deja pasar cosas que la base rechaza
// al ejecutarlas. Aquí no se comprueban resultados —la base está vacía—, se
// comprueba que CORREN. Una consulta que nunca se ha ejecutado no está probada, por
// bien que compile.

func abrirPool(t *testing.T) *pgxpool.Pool { return testdb.Open(t) }

func TestConsultasDePedidosCorren(t *testing.T) {
	pool := abrirPool(t)
	q := store.New(pool)
	ctx := context.Background()
	desde, _ := time.Parse("2006-01-02", "2026-08-10")
	hasta, _ := time.Parse("2006-01-02", "2026-08-14")

	casos := []struct {
		nombre string
		correr func() error
	}{
		// El cruce, celda a celda y día a día.
		{"VisitSummary", func() error {
			_, err := q.VisitSummary(ctx, store.VisitSummaryParams{
				FromDate: desde, ToDate: hasta, Sellers: []string{},
			})
			return err
		}},
		{"DayVisits", func() error {
			_, err := q.DayVisits(ctx, "no-existe")
			return err
		}},
		{"DaysToCross", func() error {
			_, err := q.DaysToCross(ctx, store.DaysToCrossParams{FromDate: desde, ToDate: hasta})
			return err
		}},
		{"OrdersForDay", func() error {
			_, err := q.OrdersForDay(ctx, store.OrdersForDayParams{SellerID: "no-existe", Date: desde})
			return err
		}},
		{"StopsForCross", func() error {
			_, err := q.StopsForCross(ctx, "no-existe")
			return err
		}},
		{"DeleteDayVisits", func() error {
			return q.DeleteDayVisits(ctx, "no-existe")
		}},
		{"ClearDayStopClients", func() error {
			return q.ClearDayStopClients(ctx, "no-existe")
		}},

		// Quién es quién.
		{"SellersForMatch", func() error {
			_, err := q.SellersForMatch(ctx)
			return err
		}},
		{"UnlinkedVendors", func() error {
			_, err := q.UnlinkedVendors(ctx, "")
			return err
		}},
		{"SellerLinks", func() error {
			_, err := q.SellerLinks(ctx, "")
			return err
		}},
		{"LinkOrdersToSellers", func() error {
			_, err := q.LinkOrdersToSellers(ctx)
			return err
		}},

		// La bitácora.
		{"RecentOrderSyncs", func() error {
			_, err := q.RecentOrderSyncs(ctx, 10)
			return err
		}},

		// Y las dos que explican un hueco del calendario.
		{"StuckDays", func() error {
			_, err := q.StuckDays(ctx, store.StuckDaysParams{
				FromDate: desde, ToDate: hasta, Sellers: []string{},
			})
			return err
		}},
		{"UploadStates", func() error {
			_, err := store.UploadStates(ctx, pool, "", nil, "")
			return err
		}},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if err := c.correr(); err != nil {
				t.Fatalf("%s no llega a correr contra la base: %v", c.nombre, err)
			}
		})
	}
}

// El cruce entero, con datos: una sucursal, un vendedor, un día, una parada y un
// pedido a veinte metros de ella. Es lo único que prueba de verdad que las piezas
// encajan — que el pedido se ata a su trabajador, que la visita se guarda y que la
// parada acaba llamándose como el cliente.
func TestElCruceGuardaLaVisita(t *testing.T) {
	pool := abrirPool(t)
	q := store.New(pool)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `TRUNCATE visita, vendedor_pedido, pedido, pedido_cliente,
		stop, track_day, track_point, gpx_file, drive_source, trabajador, sucursal CASCADE`); err != nil {
		t.Fatal(err)
	}

	fecha, _ := time.Parse("2006-01-02", "2026-08-12")

	if _, err := pool.Exec(ctx,
		`INSERT INTO sucursal (id, nombre, clave, codigo) VALUES ('s1', 'Camagüey', 'camaguey', 'CAM')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO trabajador (id, nombre, sucursal_id, desde) VALUES ('t1', 'ALEXANDER', 's1', '2020-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO track_day (id, trabajador_id, sucursal_id, fecha, estado) VALUES ('d1','t1','s1',$1,'OK')`,
		fecha); err != nil {
		t.Fatal(err)
	}
	// Una parada, y el cliente a unos veinte metros: dentro del radio de visita.
	if _, err := pool.Exec(ctx,
		`INSERT INTO stop (id, track_day_id, trabajador_id, sucursal_id, inicio, fin, duracion_min, lat, lon, seq)
		 VALUES ('p1','d1','t1','s1', now(), now(), 12, 21.38000, -77.91000, 0)`); err != nil {
		t.Fatal(err)
	}

	if err := q.UpsertClient(ctx, store.UpsertClientParams{
		ID: "c1", BranchID: "s1", Ref: "cli-1", Name: "Bodega La Esquina",
		Lat: 21.38015, Lon: -77.91000,
	}); err != nil {
		t.Fatal(err)
	}

	codigo, nombre := "alexander.rodriguez", "ALEXANDER RODRÍGUEZ"
	clienteID := "c1"
	if err := q.UpsertOrder(ctx, store.UpsertOrderParams{
		ID: "o1", BranchID: "s1", Ref: "ped-1", Date: fecha, ClientID: &clienteID,
		VendorCode: &codigo, VendorName: &nombre,
	}); err != nil {
		t.Fatal(err)
	}

	// Sin emparejamiento, el pedido no es de nadie y el día no se cruza.
	dias, err := q.DaysToCross(ctx, store.DaysToCrossParams{FromDate: fecha, ToDate: fecha})
	if err != nil {
		t.Fatal(err)
	}
	if len(dias) != 0 {
		t.Fatalf("sin emparejar no debería haber nada que cruzar, hay %d", len(dias))
	}

	// Y el panel lo dice en vez de callarse.
	sueltos, err := q.UnlinkedVendors(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sueltos) != 1 || sueltos[0].VendorLabel != nombre {
		t.Fatalf("el vendedor sin emparejar debería salir listado: %+v", sueltos)
	}

	// Se empareja, se ata, y entonces sí hay día que cruzar.
	if err := q.UpsertSellerLink(ctx, store.UpsertSellerLinkParams{
		ID: "v1", BranchID: "s1", SellerID: "t1",
		VendorCode: codigo, VendorName: nombre, Origin: "auto",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.LinkOrdersToSellers(ctx); err != nil {
		t.Fatal(err)
	}

	dias, err = q.DaysToCross(ctx, store.DaysToCrossParams{FromDate: fecha, ToDate: fecha})
	if err != nil {
		t.Fatal(err)
	}
	if len(dias) != 1 || dias[0].ID != "d1" {
		t.Fatalf("el día debería entrar al cruce: %+v", dias)
	}

	pedidos, err := q.OrdersForDay(ctx, store.OrdersForDayParams{SellerID: "t1", Date: fecha})
	if err != nil {
		t.Fatal(err)
	}
	if len(pedidos) != 1 || pedidos[0].ClientName != "Bodega La Esquina" {
		t.Fatalf("el pedido del día debería traer su cliente: %+v", pedidos)
	}

	// La visita, tal como la guarda el cruce.
	dist := 16.7
	parada := "p1"
	if err := q.CreateVisit(ctx, store.CreateVisitParams{
		ID: "vi1", TrackDayID: "d1", PedidoID: pedidos[0].OrderID, ClientID: "c1",
		StopID: &parada, Visited: true, DistanceM: &dist,
	}); err != nil {
		t.Fatal(err)
	}

	// Sellers vacío y no nulo, que es lo que manda el panel: `cardinality(NULL)` es
	// NULL, no cero, así que un nulo aquí haría que el filtro de alcance no case
	// NUNCA y la consulta devolviera vacío siempre.
	resumen, err := q.VisitSummary(ctx, store.VisitSummaryParams{
		FromDate: fecha, ToDate: fecha, Sellers: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumen) != 1 || resumen[0].Orders != 1 || resumen[0].Visited != 1 {
		t.Fatalf("el calendario debería ver 1 de 1 visitado: %+v", resumen)
	}

	visitas, err := q.DayVisits(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(visitas) != 1 || !visitas[0].Visited {
		t.Fatalf("el visor debería ver la visita: %+v", visitas)
	}

	// Y la parada deja de ser un punto anónimo.
	cliente := "c1"
	nombreCliente := "Bodega La Esquina"
	if err := q.SetStopClient(ctx, store.SetStopClientParams{
		ID: "p1", ClientRef: &cliente, ClientName: &nombreCliente, ClientDistM: &dist,
	}); err != nil {
		t.Fatal(err)
	}
	paradas, err := q.DayStops(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(paradas) != 1 || paradas[0].ClientName == nil || *paradas[0].ClientName != nombreCliente {
		t.Fatalf("la parada debería llamarse como su cliente: %+v", paradas)
	}
}
