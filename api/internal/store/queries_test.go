package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Estas pruebas EJECUTAN las consultas contra un Postgres de verdad, con la base
// vacía y los parámetros que manda el panel.
//
// Existen por un fallo concreto: el resumen de incidencias ordenaba por
// `sin_fichero + sin_fecha + sin_movimiento`, o sea sumando alias de salida.
// Postgres no lo permite (un alias vale suelto en el ORDER BY, no dentro de una
// expresión), pero sqlc solo comprueba la sintaxis contra el esquema y lo dio por
// bueno. El resultado: el calendario devolvía 500 desde el primer día, con el
// error escondido detrás de un "error interno" genérico.
//
// De ahí la forma de estas pruebas: no comprueban resultados, comprueban que la
// consulta CORRE. Una consulta que no se ha ejecutado nunca no está probada,
// aunque compile.
func abrir(t *testing.T) *store.Queries {
	t.Helper()
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("sin DATABASE_URL_TEST")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestConsultasDelPanelCorren(t *testing.T) {
	q := abrir(t)
	ctx := context.Background()
	desde, _ := time.Parse("2006-01-02", "2026-08-10")
	hasta, _ := time.Parse("2006-01-02", "2026-08-14")

	// Los parámetros de un super_admin sin ficha local: sucursal vacía, sin lista
	// de vendedores y sin nadie a quien excluir. Es el caso que fallaba.
	casos := []struct {
		nombre string
		correr func() error
	}{
		{"Calendar", func() error {
			_, err := q.Calendar(ctx, store.CalendarParams{FromDate: desde, ToDate: hasta})
			return err
		}},
		{"IncidentSummary", func() error {
			_, err := q.IncidentSummary(ctx, store.IncidentSummaryParams{FromDate: desde, ToDate: hasta})
			return err
		}},
		{"SellersInScope", func() error {
			_, err := q.SellersInScope(ctx, store.SellersInScopeParams{})
			return err
		}},
		{"Inbox", func() error {
			_, err := q.Inbox(ctx, store.InboxParams{LimitRows: 50})
			return err
		}},
		{"RecentScans", func() error {
			_, err := q.RecentScans(ctx, 50)
			return err
		}},
		{"ActiveSources", func() error {
			_, err := q.ActiveSources(ctx)
			return err
		}},
		{"ActiveSellers", func() error {
			_, err := q.ActiveSellers(ctx)
			return err
		}},
		{"ActiveBranches", func() error {
			_, err := q.ActiveBranches(ctx)
			return err
		}},
		{"AllAliases", func() error {
			_, err := q.AllAliases(ctx)
			return err
		}},
		{"SellerWeek", func() error {
			_, err := q.SellerWeek(ctx, store.SellerWeekParams{FromDate: desde, ToDate: hasta})
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
