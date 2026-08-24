// Package testdb da acceso a la base de pruebas a los paquetes que la necesitan.
//
// Existe por un fallo concreto: `go test ./...` corre los PAQUETES en paralelo, y
// tres de ellos —store, ingest y pedido— vacían y siembran las mismas tablas de la
// misma base. El resultado eran caídas que no tenían nada que ver con lo que se
// estaba probando: «duplicate key» al sembrar encima de otro, y un «deadlock
// detected» al truncar dos a la vez.
//
// Un cerrojo de Postgres es lo que hace falta y no más: no hay que levantar una base
// por paquete ni acordarse de pasar `-p 1`. Quien lo coge trabaja solo; los demás
// esperan su turno.
package testdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// La clave del cerrojo. Cualquier número vale mientras sea el MISMO en los tres
// paquetes: es lo único que los pone de acuerdo.
const clave int64 = 774_2026

// Open returns the pool for the test database and takes the lock; both are released
// when the test ends. Skips when there is no DATABASE_URL_TEST.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("sin DATABASE_URL_TEST")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}

	// El cerrojo va sobre UNA conexión sacada del pool y retenida: un
	// `pg_advisory_lock` pertenece a la conexión que lo pidió, y soltarlo desde otra
	// no lo suelta.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", clave); err != nil {
		conn.Release()
		pool.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", clave)
		conn.Release()
		pool.Close()
	})

	return pool
}
