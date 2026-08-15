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

	"github.com/procovar/procovar-rutas/api/internal/cola"
	"github.com/procovar/procovar-rutas/api/internal/config"
	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/ingesta"
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
	var cuentas ingesta.Cuentas = ingesta.UnaCuenta(nil)
	if credencial, err := cfg.Credenciales(); err == nil {
		if j, err := drive.CargarCuentas(credencial); err == nil {
			cuentas = j
			log.Info("cuentas de Google cargadas", "cuentas", j.Claves())
		} else {
			log.Warn("credenciales de Google ilegibles; no se podrá barrer Drive", "error", err)
		}
	} else {
		log.Warn("sin credenciales de Google; solo se consumirá la cola de n8n", "motivo", err)
	}

	svc := ingesta.NuevoServicio(pool, cuentas, log, cfg.MaxFicherosPorBarrido)

	if *ausencias {
		fecha := ayer()
		if *fechaStr != "" {
			f, err := time.Parse("2006-01-02", *fechaStr)
			if err != nil {
				log.Error("fecha inválida", "valor", *fechaStr)
				os.Exit(1)
			}
			fecha = f
		}
		n, err := svc.MarcarAusencias(ctx, fecha)
		if err != nil {
			log.Error("marcando ausencias", "error", err)
			os.Exit(1)
		}
		log.Info("ausencias marcadas", "fecha", fecha.Format("2006-01-02"), "cantidad", n)
		return
	}

	tipo := ingesta.TipoIncremental
	switch {
	case *backfill:
		tipo = ingesta.TipoBackfill
	case *nocturno:
		tipo = ingesta.TipoNocturno
	}

	if !*demonio && !*soloCola {
		correr(ctx, svc, tipo, log)
		return
	}

	// El consumidor de la cola de n8n corre en paralelo con el temporizador: los
	// ficheros que empuja n8n entran en cuanto llegan, y el barrido sigue siendo
	// la red de seguridad que se asegura de que no falte nada.
	if cfg.RedisURL != "" {
		c, err := cola.Nueva(cfg.RedisURL, cfg.PrefijoRedis)
		if err != nil {
			log.Error("Redis", "error", err)
			os.Exit(1)
		}
		defer c.Cerrar()
		if err := c.Ping(ctx); err != nil {
			log.Warn("Redis no responde; no se consumirá la cola de n8n", "error", err)
		} else {
			go ingesta.NuevoConsumidor(svc, c, log).Correr(ctx)
		}
	}

	if *soloCola {
		log.Info("solo cola: no se barrerá Drive")
		<-ctx.Done()
		return
	}

	log.Info("ingesta en marcha",
		"intervalo", cfg.IntervaloBarrido, "repaso_nocturno", cfg.HoraRepasoNocturno)

	correr(ctx, svc, tipo, log)

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
				correr(ctx, svc, ingesta.TipoNocturno, log)
				if n, err := svc.MarcarAusencias(ctx, ayer()); err == nil {
					log.Info("ausencias marcadas", "cantidad", n)
				}
				continue
			}

			correr(ctx, svc, ingesta.TipoIncremental, log)
		}
	}
}

func correr(ctx context.Context, svc *ingesta.Servicio, tipo string, log *slog.Logger) {
	inicio := time.Now()
	res, err := svc.Barrer(ctx, tipo)
	if err != nil {
		log.Error("barrido con fallos", "tipo", tipo, "error", err)
	}
	log.Info("barrido terminado",
		"tipo", tipo,
		"vistos", res.Vistos,
		"nuevos", res.Nuevos,
		"errores", res.Errores,
		"puntos", res.Puntos,
		"duracion", time.Since(inicio).Round(time.Millisecond))
}

func ayer() time.Time {
	y := time.Now().AddDate(0, 0, -1)
	return time.Date(y.Year(), y.Month(), y.Day(), 0, 0, 0, 0, time.UTC)
}
