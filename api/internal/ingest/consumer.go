package ingest

import (
	"context"
	"encoding/base64"
	"log/slog"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/queue"
)

// Consumer vacía la queue de Redis que llena n8n.
//
// Vive en el proceso de ingesta y no en la API a propósito: process un file
// puede llevar segundos —parsear miles de points, volcarlos, recalcular el día—
// y eso no puede competir por el mismo proceso que atiende el panel.
type Consumer struct {
	svc   *Service
	queue *queue.Queue
	// bus puede ser nil: los avisos en vivo son un extra, no una dependencia.
	bus *events.Bus
	log *slog.Logger
}

func NewConsumer(svc *Service, c *queue.Queue, bus *events.Bus, log *slog.Logger) *Consumer {
	return &Consumer{svc: svc, queue: c, bus: bus, log: log}
}

// avisa al panel de que algo cambió. Nunca interrumpe el job: si Redis no
// acepta el aviso, el file ya está guardado y el panel se enterará al recargar.
func (c *Consumer) avisa(ctx context.Context, e events.Event) {
	if c.bus == nil {
		return
	}
	if err := c.bus.Publish(ctx, e); err != nil {
		c.log.Warn("no se pudo publicar el aviso", "tipo", e.Type, "error", err)
	}
}

// Run consume hasta que se cancele el contexto.
func (c *Consumer) Run(ctx context.Context) {
	// Lo que quedó a medias en el último reinicio vuelve a la queue. Sin esto, un
	// despliegue en mal momento se llevaría por delante el recorrido de ese día.
	if n, err := c.queue.Recover(ctx); err != nil {
		c.log.Error("no se pudo recuperar la queue", "error", err)
	} else if n > 0 {
		c.log.Info("trabajos recuperados de un reinicio", "cantidad", n)
	}

	c.log.Info("consumidor de la queue en marcha")

	for {
		if ctx.Err() != nil {
			c.log.Info("consumidor detenido")
			return
		}

		// La espera es del propio Redis (BRPOPLPUSH bloqueante): ni sondeo ni
		// pausas artificiales. Cinco segundos para que la cancelación se note.
		job, crudo, err := c.queue.Take(ctx, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Error("leyendo de la queue", "error", err)
			time.Sleep(2 * time.Second) // Redis caído: no martillear
			continue
		}
		if job == nil {
			continue // no había nada
		}

		if err := c.process(ctx, *job); err != nil {
			c.log.Warn("job fallido",
				"file", job.Name, "intento", job.Attempts+1, "error", err)
			if err := c.queue.Fail(ctx, crudo, *job); err != nil {
				c.log.Error("no se pudo devolver el job a la queue", "error", err)
			}
			c.avisa(ctx, events.Event{Type: events.TypeQueue, Detail: "fallido"})
			continue
		}

		if err := c.queue.Finish(ctx, crudo); err != nil {
			c.log.Error("no se pudo cerrar el job", "error", err)
		}
		// Fichero dentro: la bandeja y los contadores de la queue cambian.
		c.avisa(ctx, events.Event{Type: events.TypeFile, Detail: "procesado"})
	}
}

func (c *Consumer) process(ctx context.Context, t queue.Job) error {
	contenido, err := base64.StdEncoding.DecodeString(t.ContentBase64)
	if err != nil {
		return err
	}

	nuevo, points, err := c.svc.Receive(ctx, Pushed{
		SourceID:    t.SourceID,
		FolderID:    t.FolderID,
		DriveFileID: t.DriveFileID,
		Name:        t.Name,
		FolderPath:  t.FolderPath,
		Created:     t.Created,
		Content:     contenido,
	})
	if err != nil {
		return err
	}

	c.log.Info("file de la queue procesado",
		"file", t.Name, "nuevo", nuevo, "points", points,
		"espera", time.Since(t.Queued).Round(time.Millisecond))
	return nil
}
