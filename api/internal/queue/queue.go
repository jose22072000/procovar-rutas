// Package queue is the queue of files waiting to be processed, in Redis.
//
// n8n sends the .gpx, the API enqueues it and answers straight away; the
// processing — parsing, resolving the seller, bulk-loading the points, recomputing
// the day — happens afterwards in
// the ingest process. That way a bulk upload neither keeps n8n waiting nor blocks
// the panel.
//
// EVERY key carries the `procovar-rutas:` prefix, so they never mix with PEDIDO's
// (`procovar-pedido:*`) or with those of the other systems sharing the same
// Redis.
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

// Job is a file waiting its turn.
type Job struct {
	// Account es la cuenta de Google dueña de la carpeta, o sea la sucursal.
	//
	// Faltaba, y eso hacía inútil todo el trabajo de identificar el origen: con
	// Redis en marcha los ficheros NO se procesan en el acto, se encolan, y el
	// consumidor reconstruía el empuje sin este campo. La cuenta llegaba bien al
	// API y se perdía aquí, en silencio, un paso antes de usarse.
	Account       string    `json:"account,omitempty"`
	SourceID      string    `json:"sourceId,omitempty"`
	FolderID      string    `json:"folderId,omitempty"`
	DriveFileID   string    `json:"driveFileId"`
	Name          string    `json:"name"`
	FolderPath    []string  `json:"folderPath,omitempty"`
	Created       time.Time `json:"createdAt"`
	ContentBase64 string    `json:"contentBase64"`
	Queued        time.Time `json:"queued"`
	// Attempts counts the retries, so it does not retry for ever.
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
	// The prefix ending in ":" is not cosmetic: without it, `procovar-rutas` and
	// `procovar-rutas-viejo` would share a key space.
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

// Enqueue puts a file at the end of the queue.
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

// Take pulls the next job, waiting up to `wait` when there is none.
//
// It uses BRPOPLPUSH: the job moves to a "processing" list in the same operation,
// so if the process dies halfway the file is still there and can be recovered.
// With a plain RPOP, a restart at just the wrong moment would lose that day's
// route for ever.
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
		// An unreadable item cannot keep going round: it is set aside.
		c.rdb.LRem(ctx, c.processing(), 1, crudo)
		c.rdb.LPush(ctx, c.failed(), crudo)
		return nil, "", fmt.Errorf("trabajo ilegible, apartado: %w", err)
	}
	return &t, crudo, nil
}

// Finish accepts a job as done and removes it from "processing".
func (c *Queue) Finish(ctx context.Context, crudo string) error {
	return c.rdb.LRem(ctx, c.processing(), 1, crudo).Err()
}

// MaxAttempts: past this point the file is set aside instead of retried. If it
// has not gone in after three tries, insisting will not help: what it needs is
// someone to look at it.
const MaxAttempts = 3

// Fail returns the job to the queue for another try, or sets it aside if it has
// intentó demasiadas veces.
func (c *Queue) Fail(ctx context.Context, crudo string, t Job) error {
	if err := c.rdb.LRem(ctx, c.processing(), 1, crudo).Err(); err != nil {
		return err
	}

	t.Attempts++
	if t.Attempts >= MaxAttempts {
		return c.rdb.LPush(ctx, c.failed(), crudo).Err()
	}

	datos, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.LPush(ctx, c.pending(), datos).Err()
}

// Recover returns to the queue whatever a restart left half-done. Called when
// ingest starts.
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

// Stats is what the administration screen shows.
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
