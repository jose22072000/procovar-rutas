// Paquete eventos: el aviso de "ha pasado algo" que llega al panel sin recargar.
//
// # Por qué pasa por Redis y no se queda en memoria
//
// Quien procesa los ficheros es `rutas-ingesta` y quien tiene abierta la conexión
// con el navegador es `rutas-api`: son dos contenedores distintos. Un channel en
// memoria dejaría el aviso dentro del proceso que lo genera y el panel no se
// enteraría nunca. Redis ya está ahí para la cola, así que se usa para esto.
//
// Las claves y los canales llevan el MISMO prefix que la cola
// (`procovar-rutas:`), para no mezclarse con PEDIDO en el mismo Redis.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const DefaultPrefix = "procovar-rutas:"

// Tipos de evento. Son pocos a propósito: el panel no necesita saber el detalle,
// solo que eso que está mirando cambió y conviene volver a pedirlo.
const (
	// TypeQueue: cambió el estado de la cola de n8n (pendientes/procesándose).
	TypeQueue = "queue"
	// TypeFile: entró un fichero nuevo, o uno cambió de estado.
	TypeFile = "file"
	// TypeScan: terminó un barrido de Drive.
	TypeScan = "scan"
	// TypeDay: se recalculó el día de un vendedor.
	TypeDay = "day"
)

type Event struct {
	Type     string    `json:"type"`
	SellerID string    `json:"sellerId,omitempty"`
	Date     string    `json:"date,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	At       time.Time `json:"at"`
}

type Bus struct {
	rdb     *redis.Client
	channel string
	own     bool
}

// New abre su propia conexión. Se le pasa la misma REDIS_URL y el mismo prefix
// que a la queue.
func New(url, prefix string) (*Bus, error) {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	opciones, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	return &Bus{rdb: redis.NewClient(opciones), channel: prefix + "eventos", own: true}, nil
}

func (b *Bus) Close() error {
	if b == nil || !b.own {
		return nil
	}
	return b.rdb.Close()
}

// Publish avisa. Un fallo aquí NO puede tumbar lo que se estaba haciendo: si
// Redis no está, el fichero se ha guardado igual y el panel se enterará cuando
// alguien recargue. Por eso devuelve error pero quien llama solo lo registra.
func (b *Bus) Publish(ctx context.Context, e Event) error {
	if b == nil {
		return nil
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	datos, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, b.channel, datos).Err()
}

// Subscribe devuelve un channel con los eventos hasta que se cancele el contexto.
//
// El channel tiene hueco para unos cuantos: si quien lee se atasca, se tiran los
// nuevos en vez de bloquear a todo el mundo. Perder un aviso no es grave —son
// "vuelve a pedir los datos", no los datos—, pero atascar la publicación sí.
func (b *Bus) Subscribe(ctx context.Context) (<-chan Event, error) {
	if b == nil {
		return nil, fmt.Errorf("sin bus de eventos")
	}
	sub := b.rdb.Subscribe(ctx, b.channel)
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, err
	}

	out := make(chan Event, 16)
	go func() {
		defer close(out)
		defer sub.Close()
		for msg := range sub.Channel() {
			var e Event
			if err := json.Unmarshal([]byte(msg.Payload), &e); err != nil {
				continue
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			default:
				// Lector lento: se descarta este aviso.
			}
		}
	}()
	return out, nil
}
