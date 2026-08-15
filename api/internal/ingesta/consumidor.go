package ingesta

import (
	"context"
	"encoding/base64"
	"log/slog"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/cola"
)

// Consumidor vacía la cola de Redis que llena n8n.
//
// Vive en el proceso de ingesta y no en la API a propósito: procesar un fichero
// puede llevar segundos —parsear miles de puntos, volcarlos, recalcular el día—
// y eso no puede competir por el mismo proceso que atiende el panel.
type Consumidor struct {
	svc  *Servicio
	cola *cola.Cola
	log  *slog.Logger
}

func NuevoConsumidor(svc *Servicio, c *cola.Cola, log *slog.Logger) *Consumidor {
	return &Consumidor{svc: svc, cola: c, log: log}
}

// Correr consume hasta que se cancele el contexto.
func (c *Consumidor) Correr(ctx context.Context) {
	// Lo que quedó a medias en el último reinicio vuelve a la cola. Sin esto, un
	// despliegue en mal momento se llevaría por delante el recorrido de ese día.
	if n, err := c.cola.Recuperar(ctx); err != nil {
		c.log.Error("no se pudo recuperar la cola", "error", err)
	} else if n > 0 {
		c.log.Info("trabajos recuperados de un reinicio", "cantidad", n)
	}

	c.log.Info("consumidor de la cola en marcha")

	for {
		if ctx.Err() != nil {
			c.log.Info("consumidor detenido")
			return
		}

		// La espera es del propio Redis (BRPOPLPUSH bloqueante): ni sondeo ni
		// pausas artificiales. Cinco segundos para que la cancelación se note.
		trabajo, crudo, err := c.cola.Tomar(ctx, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log.Error("leyendo de la cola", "error", err)
			time.Sleep(2 * time.Second) // Redis caído: no martillear
			continue
		}
		if trabajo == nil {
			continue // no había nada
		}

		if err := c.procesar(ctx, *trabajo); err != nil {
			c.log.Warn("trabajo fallido",
				"fichero", trabajo.Nombre, "intento", trabajo.Intentos+1, "error", err)
			if err := c.cola.Fallar(ctx, crudo, *trabajo); err != nil {
				c.log.Error("no se pudo devolver el trabajo a la cola", "error", err)
			}
			continue
		}

		if err := c.cola.Terminar(ctx, crudo); err != nil {
			c.log.Error("no se pudo cerrar el trabajo", "error", err)
		}
	}
}

func (c *Consumidor) procesar(ctx context.Context, t cola.Trabajo) error {
	contenido, err := base64.StdEncoding.DecodeString(t.ContenidoBase64)
	if err != nil {
		return err
	}

	nuevo, puntos, err := c.svc.Recibir(ctx, Empujado{
		FuenteID:    t.FuenteID,
		FolderID:    t.FolderID,
		DriveFileID: t.DriveFileID,
		Nombre:      t.Nombre,
		RutaCarpeta: t.RutaCarpeta,
		Creado:      t.Creado,
		Contenido:   contenido,
	})
	if err != nil {
		return err
	}

	c.log.Info("fichero de la cola procesado",
		"fichero", t.Nombre, "nuevo", nuevo, "puntos", puntos,
		"espera", time.Since(t.Encolado).Round(time.Millisecond))
	return nil
}
