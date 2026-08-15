// Comando ingesta: baja los .gpx de las carpetas de Drive.
//
//	ingesta                 barrido incremental, una vez
//	ingesta --demonio       incremental cada N minutos + repaso nocturno
//	ingesta --nocturno      repaso completo, ignorando el cursor
//	ingesta --backfill      todo el histórico (primer arranque)
//	ingesta --ausencias     marca los SIN_FICHERO del día indicado (--fecha)
//
// Va aparte de la API a propósito: un barrido largo no puede bloquear las
// peticiones del panel, y así se puede reiniciar o lanzar a mano sin tocar el
// servidor.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/config"
	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/queue"
)

func main() {
	demonio := flag.Bool("demonio", false, "quedarse corriendo con el temporizador")
	nocturno := flag.Bool("nocturno", false, "repaso completo, ignorando el cursor")
	backfill := flag.Bool("backfill", false, "ingerir todo el histórico")
	ausencias := flag.Bool("ausencias", false, "marcar los días sin fichero")
	soloCola := flag.Bool("solo-cola", false, "solo consumir la cola de n8n, sin barrer Drive")
	fechaStr := flag.String("fecha", "", "fecha YYYY-MM-DD para --ausencias (por defecto, ayer)")
	flag.Parse()

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

	// Las credenciales de Google solo hacen falta para BARRER. Si la entrada es
	// solo por n8n, este proceso sigue siendo útil —consume la cola y marca las
	// ausencias— sin tener acceso a Drive, así que no se sale por eso.
	var cuentas ingest.Accounts = ingest.SingleAccount(nil)
	if credencial, err := cfg.Credenciales(); err == nil {
		if j, err := drive.LoadAccounts(credencial); err == nil {
			cuentas = j
			log.Info("cuentas de Google cargadas", "cuentas", j.Keys())
		} else {
			log.Warn("credenciales de Google ilegibles; no se podrá barrer Drive", "error", err)
		}
	} else {
		log.Warn("sin credenciales de Google; solo se consumirá la cola de n8n", "motivo", err)
	}

	svc := ingest.NewService(pool, cuentas, log, cfg.MaxFicherosPorBarrido)

	if *ausencias {
		fecha := yesterday()
		if *fechaStr != "" {
			f, err := time.Parse("2006-01-02", *fechaStr)
			if err != nil {
				log.Error("fecha inválida", "valor", *fechaStr)
				os.Exit(1)
			}
			fecha = f
		}
		n, err := svc.MarkAbsences(ctx, fecha)
		if err != nil {
			log.Error("marcando ausencias", "error", err)
			os.Exit(1)
		}
		log.Info("ausencias marcadas", "fecha", fecha.Format("2006-01-02"), "cantidad", n)
		return
	}

	tipo := ingest.TypeIncremental
	switch {
	case *backfill:
		tipo = ingest.TipoBackfill
	case *nocturno:
		tipo = ingest.TypeNightly
	}

	if !*demonio && !*soloCola {
		run(ctx, svc, tipo, log)
		return
	}

	// El consumidor de la cola de n8n corre en paralelo con el temporizador: los
	// ficheros que empuja n8n entran en cuanto llegan, y el barrido sigue siendo
	// la red de seguridad que se asegura de que no falte nada.
	if cfg.RedisURL != "" {
		c, err := queue.New(cfg.RedisURL, cfg.PrefijoRedis)
		if err != nil {
			log.Error("Redis", "error", err)
			os.Exit(1)
		}
		defer c.Close()
		if err := c.Ping(ctx); err != nil {
			log.Warn("Redis no responde; no se consumirá la cola de n8n", "error", err)
		} else {
			// Mismo Redis y mismo prefijo para los avisos del panel.
			var bus *events.Bus
			if b, err := events.New(cfg.RedisURL, cfg.PrefijoRedis); err != nil {
				log.Warn("sin avisos en vivo", "error", err)
			} else {
				bus = b
				defer b.Close()
			}
			go ingest.NewConsumer(svc, c, bus, log).Run(ctx)
		}
	}

	if *soloCola {
		log.Info("solo queue: no se barrerá Drive")
		<-ctx.Done()
		return
	}

	log.Info("ingesta en marcha",
		"intervalo", cfg.IntervaloBarrido, "repaso_nocturno", cfg.HoraRepasoNocturno)

	run(ctx, svc, tipo, log)

	ticker := time.NewTicker(cfg.IntervaloBarrido)
	defer ticker.Stop()
	ultimoNocturno := ""

	for {
		select {
		case <-ctx.Done():
			log.Info("ingesta detenida")
			return
		case <-ticker.C:
			ahora := time.Now()
			hoy := ahora.Format("2006-01-02")

			// Un repaso completo al día. Es lo que garantiza que no falte nada
			// aunque el incremental se haya saltado un fichero por llegar con la
			// fecha de modificación cambiada, renombrado o movido de carpeta.
			if ahora.Hour() == cfg.HoraRepasoNocturno && ultimoNocturno != hoy {
				ultimoNocturno = hoy
				run(ctx, svc, ingest.TypeNightly, log)
				if n, err := svc.MarkAbsences(ctx, yesterday()); err == nil {
					log.Info("ausencias marcadas", "cantidad", n)
				}
				continue
			}

			run(ctx, svc, ingest.TypeIncremental, log)
		}
	}
}

func run(ctx context.Context, svc *ingest.Service, tipo string, log *slog.Logger) {
	inicio := time.Now()
	res, err := svc.Scan(ctx, tipo)
	if err != nil {
		log.Error("barrido con fallos", "tipo", tipo, "error", err)
	}
	log.Info("barrido terminado",
		"tipo", tipo,
		"vistos", res.Seen,
		"nuevos", res.New,
		"errores", res.Failed,
		"puntos", res.Points,
		"duracion", time.Since(inicio).Round(time.Millisecond))
}

func yesterday() time.Time {
	y := time.Now().AddDate(0, 0, -1)
	return time.Date(y.Year(), y.Month(), y.Day(), 0, 0, 0, 0, time.UTC)
}
