// Comando api: el servidor HTTP del panel de rutas.
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
	"github.com/procovar/procovar-rutas/api/internal/cola"
	"github.com/procovar/procovar-rutas/api/internal/config"
	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/ingesta"
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

	// El panel puede arrancar sin credencial de Google: sirve el histórico que ya
	// está en la base. Lo que no funcionará es lanzar un barrido a mano, y el
	// arranque lo dice en vez de fallar más tarde con un error críptico.
	var cuentas ingesta.Cuentas
	if credencial, err := cfg.Credenciales(); err == nil {
		if j, err := drive.CargarCuentas(credencial); err == nil {
			cuentas = j
			log.Info("cuentas de Google cargadas", "cuentas", j.Claves())
		} else {
			log.Warn("credenciales de Google ilegibles; el panel servirá solo lo ya ingerido", "error", err)
		}
	} else {
		log.Warn("sin credenciales de Google; el barrido manual no estará disponible", "motivo", err)
	}
	if cuentas == nil {
		cuentas = ingesta.UnaCuenta(nil)
	}

	// Sin Redis el sistema sigue funcionando: el empuje de n8n se procesa en el
	// acto en vez de encolarse. Peor bajo carga, pero preferible a no arrancar.
	var colaRedis *cola.Cola
	if cfg.RedisURL != "" {
		c, err := cola.Nueva(cfg.RedisURL, cfg.PrefijoRedis)
		if err != nil {
			log.Error("Redis", "error", err)
			os.Exit(1)
		}
		if err := c.Ping(ctx); err != nil {
			log.Warn("Redis no responde; el empuje de n8n se procesará en el acto", "error", err)
		} else {
			colaRedis = c
			defer c.Cerrar()
			log.Info("cola en Redis lista", "prefijo", cfg.PrefijoRedis)
		}
	} else {
		log.Warn("sin REDIS_URL; el empuje de n8n se procesará en el acto")
	}

	svcIngesta := ingesta.NuevoServicio(pool, cuentas, log, cfg.MaxFicherosPorBarrido)
	servidor := api.NuevoServidor(cfg, pool, cliAuth, svcIngesta, colaRedis, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Puerto,
		Handler:           servidor.Rutas(),
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
