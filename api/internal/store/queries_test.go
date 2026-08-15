package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// These tests EXECUTE the queries against a real Postgres, with an empty database
// and the parameters the panel sends.
//
// They exist because of one concrete bug: the incident summary ordered by
// `sin_fichero + sin_fecha + sin_movimiento`, that is, by summing output aliases.
// Postgres does not allow that (an alias works alone in ORDER BY, not inside an
// expression), but sqlc only checks the syntax against the schema and passed it.
// The result: the calendar returned 500 from day one, with the error hidden behind
// a generic "internal error".
//
// Hence the shape of these tests: they do not check results, they check that the
// query RUNS. A query that has never been executed is not tested, however well it
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

	// A super_admin's parameters with no local record: empty branch, no seller list
	// and nobody to exclude. That is the case that failed.
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
