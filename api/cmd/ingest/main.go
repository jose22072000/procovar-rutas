// Command ingest: pulls the .gpx files from the Drive folders.
//
//	ingest                  incremental scan, once
//	ingesta --daemon       incremental cada N minutos + repaso nightly
//	ingest --nightly        full sweep, ignoring the cursor
//	ingest --backfill       the entire history (first start)
//	ingest --absences       marks the day's SIN_FICHERO rows (--date)
//
// It is deliberately separate from the API: a long scan cannot block the panel's
// requests, and this way it can be restarted or run by hand without touching the
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
	"github.com/procovar/procovar-rutas/api/internal/pedido"
	"github.com/procovar/procovar-rutas/api/internal/queue"
)

func main() {
	daemon := flag.Bool("daemon", false, "keep running on the timer")
	nightly := flag.Bool("nightly", false, "full sweep, ignoring the cursor")
	backfill := flag.Bool("backfill", false, "ingest the entire history")
	absences := flag.Bool("absences", false, "mark the days with no file")
	queueOnly := flag.Bool("queue-only", false, "only drain n8n's queue, without scanning Drive")
	dateStr := flag.String("date", "", "YYYY-MM-DD date for --absences (defaults to yesterday)")
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

	// Google credentials are only needed for SCANNING. If files only arrive through
	// n8n, this process is still useful — it drains the queue and marks absences —
	// without any Drive access, so it does not quit over that.
	var cuentas ingest.Accounts = ingest.SingleAccount(nil)
	if credencial, err := cfg.Credentials(); err == nil {
		if j, err := drive.LoadAccounts(credencial); err == nil {
			cuentas = j
			log.Info("cuentas de Google cargadas", "cuentas", j.Keys())
		} else {
			log.Warn("credenciales de Google ilegibles; no se podrá barrer Drive", "error", err)
		}
	} else {
		log.Warn("sin credenciales de Google; solo se consumirá la cola de n8n", "motivo", err)
	}

	// Los avisos en vivo se montan aquí arriba y no dentro del modo demonio: los usan
	// tanto el consumidor de la cola como la sincronización de pedidos, y montarlos
	// dos veces serían dos conexiones a Redis para lo mismo.
	var bus *events.Bus
	if cfg.RedisURL != "" {
		if b, err := events.New(cfg.RedisURL, cfg.PrefijoRedis); err != nil {
			log.Warn("sin avisos en vivo", "error", err)
		} else {
			bus = b
			defer b.Close()
		}
	}

	svc := ingest.NewService(pool, cuentas, log, cfg.MaxFicherosPorBarrido)

	// El cruce con PEDIDO, si está configurado. Se engancha a la ingesta: cada día
	// que se recalcula trae paradas nuevas, y con paradas nuevas cambia a quién
	// visitó. Sin PEDIDO_API_URL esto queda apagado y la ingesta es la de siempre.
	var svcPedidos *pedido.Service
	if cli := pedido.NewClient(cfg.PedidoURL, cfg.PedidoKey); cli != nil {
		svcPedidos = pedido.NewService(pool, cli, bus, log, cfg.PedidoVentanaDias)
		svc.TrasRecalcular = func(ctx context.Context, trackDayID, trabajadorID, sucursalID string, fecha time.Time) {
			if err := svcPedidos.CrossOneDay(ctx, trackDayID, trabajadorID, sucursalID, fecha); err != nil {
				log.Warn("no se pudo cruzar el día con los pedidos",
					"dia", trackDayID, "fecha", fecha.Format("2006-01-02"), "error", err)
			}
		}
		log.Info("cruce con PEDIDO listo", "url", cfg.PedidoURL, "ventana_dias", cfg.PedidoVentanaDias)
	} else {
		log.Warn("sin PEDIDO_API_URL: no se cruzarán los pedidos con las rutas")
	}

	if *absences {
		fecha := yesterday()
		if *dateStr != "" {
			f, err := time.Parse("2006-01-02", *dateStr)
			if err != nil {
				log.Error("fecha inválida", "valor", *dateStr)
				os.Exit(1)
			}
			fecha = f
		}
		n, err := svc.MarkAbsences(ctx, fecha)
		if err != nil {
			log.Error("marcando absences", "error", err)
			os.Exit(1)
		}
		log.Info("absences marcadas", "fecha", fecha.Format("2006-01-02"), "cantidad", n)
		return
	}

	tipo := ingest.TypeIncremental
	switch {
	case *backfill:
		tipo = ingest.TipoBackfill
	case *nightly:
		tipo = ingest.TypeNightly
	}

	if !*daemon && !*queueOnly {
		run(ctx, svc, tipo, log)
		return
	}

	// The consumer of n8n's queue runs alongside the timer: the files n8n pushes go
	// in as soon as they arrive, and the scan remains the safety net that makes sure
	// nothing is missing.
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
			go ingest.NewConsumer(svc, c, bus, log).Run(ctx)
		}
	}

	if *queueOnly {
		log.Info("solo queue: no se barrerá Drive")
		<-ctx.Done()
		return
	}

	log.Info("ingesta en marcha",
		"intervalo", cfg.IntervaloBarrido, "repaso_nightly", cfg.HoraRepasoNocturno)

	// Los pedidos van por su propio reloj, más lento que el de Drive: un .gpx aparece
	// cuando el vendedor termina el día, pero los pedidos ya estaban puestos por la
	// mañana. Cada hora es de sobra, y así no se le hacen a PEDIDO cuarenta y ocho
	// consultas diarias para traer lo mismo.
	if svcPedidos != nil {
		go sincronizarPedidos(ctx, svcPedidos, cfg.IntervaloPedidos, log)
	}

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

			// One full sweep a day. It is what guarantees nothing is missing even
			// if the incremental scan skipped a file because it arrived renamed,
			// moved, or with a changed modification date.
			if ahora.Hour() == cfg.HoraRepasoNocturno && ultimoNocturno != hoy {
				ultimoNocturno = hoy
				run(ctx, svc, ingest.TypeNightly, log)
				if n, err := svc.MarkAbsences(ctx, yesterday()); err == nil {
					log.Info("absences marcadas", "cantidad", n)
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

// sincronizarPedidos trae los pedidos de PEDIDO cada tanto y vuelve a cruzarlos.
//
// La primera pasada es inmediata: al arrancar el contenedor lo que se quiere es que
// el panel tenga los pedidos de hoy, no dentro de una hora.
func sincronizarPedidos(ctx context.Context, svc *pedido.Service, cada time.Duration, log *slog.Logger) {
	correr := func() {
		inicio := time.Now()
		res, err := svc.Sync(ctx, "programado")
		if err != nil {
			log.Error("no se pudieron traer los pedidos", "error", err)
			return
		}
		log.Info("pedidos sincronizados",
			"pedidos", res.Orders,
			"clientes", res.Clients,
			"dias_cruzados", res.Crosses,
			"emparejados", res.Linked,
			"duracion", time.Since(inicio).Round(time.Millisecond))
	}

	correr()

	t := time.NewTicker(cada)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			correr()
		}
	}
}
