// Package cola es la cola de ficheros pending de procesar, en Redis.
//
// n8n manda el .gpx, la API lo encola y contesta enseguida; el procesado —parsear,
// resolver el vendedor, volcar los puntos, recalcular el día— ocurre después en
// el proceso de ingest. Así una subida masiva no deja a n8n esperando ni bloquea
// el panel.
//
// TODAS las claves llevan el prefix `procovar-rutas:`, para no mezclarse con las
// de PEDIDO (`procovar-pedido:*`) ni con las de los demás sistemas que comparten
// el mismo Redis.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const DefaultPrefix = "procovar-rutas:"

// Job es un fichero esperando turno.
type Job struct {
	SourceID      string    `json:"fuenteId,omitempty"`
	FolderID      string    `json:"folderId,omitempty"`
	DriveFileID   string    `json:"driveFileId"`
	Name          string    `json:"name"`
	FolderPath    []string  `json:"rutaCarpeta,omitempty"`
	Created       time.Time `json:"createdAt"`
	ContentBase64 string    `json:"contentBase64"`
	Queued        time.Time `json:"queued"`
	// Attempts cuenta las veces que se ha reintentado, para no reintentar sin fin.
	Attempts int `json:"attempts"`
}

type Queue struct {
	rdb    *redis.Client
	prefix string
}

func New(url, prefix string) (*Queue, error) {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	// Que el prefix termine en ":" no es cosmético: sin él, `procovar-rutas` y
	// `procovar-rutas-viejo` compartirían espacio de claves.
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}

	opciones, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	return &Queue{rdb: redis.NewClient(opciones), prefix: prefix}, nil
}

func (c *Queue) key(nombre string) string { return c.prefix + nombre }

func (c *Queue) pending() string    { return c.key("ingesta:pending") }
func (c *Queue) processing() string { return c.key("ingesta:processing") }
func (c *Queue) failed() string     { return c.key("ingesta:failed") }

func (c *Queue) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }
func (c *Queue) Close() error                   { return c.rdb.Close() }

// Enqueue mete un fichero al final de la queue.
func (c *Queue) Enqueue(ctx context.Context, t Job) error {
	if t.Queued.IsZero() {
		t.Queued = time.Now().UTC()
	}
	datos, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.LPush(ctx, c.pending(), datos).Err()
}

// Take saca el siguiente trabajo, esperando hasta `espera` si no hay ninguno.
//
// Usa BRPOPLPUSH: el trabajo pasa a una lista "processing" en la misma
// operación, así que si el proceso se cae a mitad, el fichero sigue ahí y se
// puede recuperar. Con un RPOP normal, un reinicio en el momento justo perdería
// el recorrido de ese día para siempre.
func (c *Queue) Take(ctx context.Context, espera time.Duration) (*Job, string, error) {
	crudo, err := c.rdb.BRPopLPush(ctx, c.pending(), c.processing(), espera).Result()
	if err == redis.Nil {
		return nil, "", nil // no había nada
	}
	if err != nil {
		return nil, "", err
	}

	var t Job
	if err := json.Unmarshal([]byte(crudo), &t); err != nil {
		// Un elemento ilegible no puede quedarse dando vueltas: se aparta.
		c.rdb.LRem(ctx, c.processing(), 1, crudo)
		c.rdb.LPush(ctx, c.failed(), crudo)
		return nil, "", fmt.Errorf("trabajo ilegible, apartado: %w", err)
	}
	return &t, crudo, nil
}

// Finish da por bueno un trabajo y lo quita de "processing".
func (c *Queue) Finish(ctx context.Context, crudo string) error {
	return c.rdb.LRem(ctx, c.processing(), 1, crudo).Err()
}

// MaxIntentos: a partir de aquí el fichero se aparta en vez de reintentarse.
// Si tres veces no ha entrado, no va a entrar por insistir: lo que hace falta es
// que alguien lo mire.
const MaxIntentos = 3

// Fail devuelve el trabajo a la cola para reintentarlo, o lo aparta si ya se
// intentó demasiadas veces.
func (c *Queue) Fail(ctx context.Context, crudo string, t Job) error {
	if err := c.rdb.LRem(ctx, c.processing(), 1, crudo).Err(); err != nil {
		return err
	}

	t.Attempts++
	if t.Attempts >= MaxIntentos {
		return c.rdb.LPush(ctx, c.failed(), crudo).Err()
	}

	datos, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.LPush(ctx, c.pending(), datos).Err()
}

// Recover devuelve a la cola lo que quedó a medias en un reinicio.
// Se llama al arrancar la ingest.
func (c *Queue) Recover(ctx context.Context) (int, error) {
	n := 0
	for {
		crudo, err := c.rdb.RPopLPush(ctx, c.processing(), c.pending()).Result()
		if err == redis.Nil {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		_ = crudo
		n++
	}
}

// Stats es lo que enseña la pantalla de administración.
type Stats struct {
	Pending    int64 `json:"pending"`
	Processing int64 `json:"processing"`
	Failed     int64 `json:"failed"`
}

func (c *Queue) Stats(ctx context.Context) (Stats, error) {
	var e Stats
	var err error
	if e.Pending, err = c.rdb.LLen(ctx, c.pending()).Result(); err != nil {
		return e, err
	}
	if e.Processing, err = c.rdb.LLen(ctx, c.processing()).Result(); err != nil {
		return e, err
	}
	if e.Failed, err = c.rdb.LLen(ctx, c.failed()).Result(); err != nil {
		return e, err
	}
	return e, nil
}
