// Command migrate: applies and rolls back the database schema.
//
// It uses golang-migrate as a library, not as a separate binary: that way the
// deployment ships one executable and nobody has to remember to install a tool on
// the server. The migrations are EMBEDDED in the binary with go:embed, so a binary
// can never be deployed without its migrations, nor migrations applied that do not
// belong to that binary.
//
//	migrate up          applies everything pending
//	migrate down 1      rolls back the last one
//	migrate version     says which version the database is on
//	migrate force 3     unsticks a migration marked dirty
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/procovar/procovar-rutas/api/migrations"
)

func main() {
	log.SetFlags(0)

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("falta DATABASE_URL")
	}

	orden := "up"
	if len(os.Args) > 1 {
		orden = os.Args[1]
	}

	// The database creates itself if it does not exist.
	//
	// Postgres does not create it on connect, so without this the first deploy dies
	// with "database does not exist" and someone has to ssh into the server and run
	// a CREATE DATABASE. It is idempotent: if it is already there, it does nothing.
	if err := createDatabaseIfMissing(url); err != nil {
		log.Fatalf("no se pudo preparar la base: %v", err)
	}

	fuente, err := iofs.New(migrations.FS, ".")
	if err != nil {
		log.Fatalf("no se pudieron leer las migraciones empotradas: %v", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", fuente, url)
	if err != nil {
		log.Fatalf("no se pudo conectar con la base: %v", err)
	}
	defer m.Close()

	switch orden {
	case "up":
		err = m.Up()
	case "down":
		pasos := 1
		if len(os.Args) > 2 {
			pasos, err = strconv.Atoi(os.Args[2])
			if err != nil {
				log.Fatalf("número de pasos inválido: %v", err)
			}
		}
		// Always step by step, never a whole m.Down(): a full `down` on the wrong
		// server wipes the database without asking.
		err = m.Steps(-pasos)
	case "version":
		v, sucia, verr := m.Version()
		if errors.Is(verr, migrate.ErrNilVersion) {
			fmt.Println("base sin migrar")
			return
		}
		if verr != nil {
			log.Fatalf("no se pudo leer la versión: %v", verr)
		}
		estado := "limpia"
		if sucia {
			estado = "SUCIA — hay que revisar a mano y luego `migrar force <v>`"
		}
		fmt.Printf("versión %d (%s)\n", v, estado)
		return
	case "force":
		if len(os.Args) < 3 {
			log.Fatal("uso: migrar force <version>")
		}
		v, cerr := strconv.Atoi(os.Args[2])
		if cerr != nil {
			log.Fatalf("versión inválida: %v", cerr)
		}
		err = m.Force(v)
	default:
		log.Fatalf("orden desconocida: %q (up | down [n] | version | force <v>)", orden)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("sin cambios: la base ya estaba al día")
		return
	}
	if err != nil {
		log.Fatalf("la migración falló: %v", err)
	}

	fmt.Println("migración aplicada")
}

// createDatabaseIfMissing connects to the `postgres` maintenance database on the
// same server and creates the project's one if it does not exist yet.
func createDatabaseIfMissing(url string) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("DATABASE_URL ilegible: %w", err)
	}
	objetivo := cfg.Database
	if objetivo == "" {
		return fmt.Errorf("DATABASE_URL no dice qué base usar")
	}

	// Same connection, but to `postgres`: the maintenance database that always
	// exists and from which another can be created.
	admin := *cfg
	admin.Database = "postgres"

	ctx, cancelar := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelar()

	conn, err := pgx.ConnectConfig(ctx, &admin)
	if err != nil {
		// The user may have no access to `postgres` but plenty to their own. Let it
		// pass: if the database exists, the migration will work anyway.
		fmt.Printf("aviso: no se pudo comprobar si la base existe (%v); se sigue\n", err)
		return nil
	}
	defer conn.Close(context.Background())

	var existe bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", objetivo).Scan(&existe); err != nil {
		return fmt.Errorf("consultando bases: %w", err)
	}
	if existe {
		return nil
	}

	// The name cannot be a parameter in a CREATE DATABASE, so it is quoted with
	// Postgres identifier rules instead of being concatenated raw.
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{objetivo}.Sanitize()); err != nil {
		// `api` and `ingest` start at the same time and share an image: both can see
		// the database is missing and issue the CREATE. Whoever arrives second gets
		// "duplicate_database", which is exactly the result it was after.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P04" {
			return nil
		}
		return fmt.Errorf("creando la base %q: %w", objetivo, err)
	}
	fmt.Printf("base %q creada\n", objetivo)
	return nil
}
