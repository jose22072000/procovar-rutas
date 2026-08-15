// Command api: the routes panel's HTTP server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/api"
	"github.com/procovar/procovar-rutas/api/internal/auth"
	"github.com/procovar/procovar-rutas/api/internal/config"
	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/queue"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Cargar()
	if err != nil {
		log.Error("configuración", "error", err)
		os.Exit(1)
	}

	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("no se pudo conectar con Postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("Postgres no responde", "error", err)
		os.Exit(1)
	}

	cliAuth, err := auth.NuevoCliente(cfg.AuthURL, cfg.AuthClientID, cfg.AuthSigningKey, 1)
	if err != nil {
		log.Error("procovar-auth", "error", err)
		os.Exit(1)
	}

	// The panel can start without a Google credential: it serves the history already
	// in the database. What will not work is launching a scan by hand, and the
	// startup says so instead of failing later with a cryptic error.
	var cuentas ingest.Accounts
	if credencial, err := cfg.Credentials(); err == nil {
		if j, err := drive.LoadAccounts(credencial); err == nil {
			cuentas = j
			log.Info("cuentas de Google cargadas", "cuentas", j.Keys())
		} else {
			log.Warn("credenciales de Google ilegibles; el panel servirá solo lo ya ingerido", "error", err)
		}
	} else {
		log.Warn("sin credenciales de Google; el barrido manual no estará disponible", "motivo", err)
	}
	if cuentas == nil {
		cuentas = ingest.SingleAccount(nil)
	}

	// Without Redis the system still works: n8n's push is processed on the spot
	// instead of being queued. Worse under load, but better than refusing to start.
	var colaRedis *queue.Queue
	if cfg.RedisURL != "" {
		c, err := queue.New(cfg.RedisURL, cfg.PrefijoRedis)
		if err != nil {
			log.Error("Redis", "error", err)
			os.Exit(1)
		}
		if err := c.Ping(ctx); err != nil {
			log.Warn("Redis no responde; el empuje de n8n se procesará en el acto", "error", err)
		} else {
			colaRedis = c
			defer c.Close()
			log.Info("cola en Redis lista", "prefijo", cfg.PrefijoRedis)
		}
	} else {
		log.Warn("sin REDIS_URL; el empuje de n8n se procesará en el acto")
	}

	// The same Redis and the same prefix as the queue, for the panel's live
	// notifications (SSE). With no Redis there are no notifications and the panel
	// reloads by hand: annoying, but not a reason to refuse to start.
	var bus *events.Bus
	if cfg.RedisURL != "" {
		b, err := events.New(cfg.RedisURL, cfg.PrefijoRedis)
		if err != nil {
			log.Warn("sin avisos en vivo", "error", err)
		} else {
			bus = b
			defer b.Close()
			log.Info("avisos en vivo listos", "prefijo", cfg.PrefijoRedis)
		}
	}

	svcIngesta := ingest.NewService(pool, cuentas, log, cfg.MaxFicherosPorBarrido)
	servidor := api.NewServer(cfg, pool, cliAuth, svcIngesta, colaRedis, bus, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Puerto,
		Handler:           servidor.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("panel de rutas escuchando", "puerto", cfg.Puerto, "entorno", cfg.Entorno)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("el servidor se cayó", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("cerrando")

	cierre, cancelar := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelar()
	if err := srv.Shutdown(cierre); err != nil {
		log.Error("cierre forzado", "error", err)
	}
}
