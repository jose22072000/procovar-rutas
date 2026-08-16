package ingest

import (
	"context"
	"encoding/base64"
	"log/slog"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/queue"
)

// Consumer drains the Redis queue that n8n fills.
//
// It deliberately lives in the ingest process and not in the API: processing one
// file can take seconds — parsing thousands of points, bulk-loading them,
// recomputing the day — and that cannot compete for the same process that serves
// the panel.
type Consumer struct {
	svc   *Service
	queue *queue.Queue
	// bus may be nil: live notifications are an extra, not a dependency.
	bus *events.Bus
	log *slog.Logger
}

func NewConsumer(svc *Service, c *queue.Queue, bus *events.Bus, log *slog.Logger) *Consumer {
	return &Consumer{svc: svc, queue: c, bus: bus, log: log}
}

// notify tells the panel something changed. It never interrupts the job: if Redis
// refuses the notification the file is already stored and the panel will find out
// on the next reload.
func (c *Consumer) notify(ctx context.Context, e events.Event) {
	if c.bus == nil {
		return
	}
	if err := c.bus.Publish(ctx, e); err != nil {
		c.log.Warn("no se pudo publicar el aviso", "tipo", e.Type, "error", err)
	}
}

// Run consumes until the context is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	// Whatever was left half-done by the last restart goes back on the queue.
	// Without this, a deploy at the wrong moment would take that day's route with
	// it.
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

		// Redis itself does the waiting (blocking BRPOPLPUSH): no polling and no
		// artificial sleeps. Five seconds so cancellation is noticed.
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
			c.notify(ctx, events.Event{Type: events.TypeQueue, Detail: "fallido"})
			continue
		}

		if err := c.queue.Finish(ctx, crudo); err != nil {
			c.log.Error("no se pudo cerrar el job", "error", err)
		}
		// File is in: the inbox and the queue counters change.
		c.notify(ctx, events.Event{Type: events.TypeFile, Detail: "procesado"})
	}
}

func (c *Consumer) process(ctx context.Context, t queue.Job) error {
	contenido, err := base64.StdEncoding.DecodeString(t.ContentBase64)
	if err != nil {
		return err
	}

	nuevo, points, err := c.svc.Receive(ctx, Pushed{
		Account:     t.Account,
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
