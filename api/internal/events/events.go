// Package events carries the "something happened" notice that reaches the panel
// without a reload.
// # Why this goes through Redis instead of staying in memory
//
// Files are processed by `rutas-ingesta`, while the connection to the browser is
// held open by `rutas-api`: two different containers. An in-memory channel would
// leave the notice inside the process that produced it and the panel would never
// find out. Redis is already there for the queue, so it is used for this too.
//
// The keys and channels carry the SAME prefix as the queue (`procovar-rutas:`), so
// they never mix with PEDIDO in the same Redis.
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

// Event types. Deliberately few: the panel does not need the detail, only that
// what it is looking at changed and is worth asking for again.
const (
	// TypeQueue: the state of n8n's queue changed (pending/processing).
	TypeQueue = "queue"
	// TypeFile: a new file came in, or one changed state.
	TypeFile = "file"
	// TypeScan: a Drive scan finished.
	TypeScan = "scan"
	// TypeDay: a seller's day was recomputed.
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

// New opens its own connection. It takes the same REDIS_URL and the same prefix as
// the queue.
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

// Publish sends the notice. A failure here must NOT bring down whatever was being
// done: if Redis is away, the file was still stored and the panel will find out
// when someone reloads. That is why it returns an error but callers only log it.
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

// Subscribe returns a channel of events until the context is cancelled.
//
// The channel has room for a handful: if the reader stalls, new ones are dropped
// rather than blocking everybody. Losing a notice is not serious — they say "ask
// for the data again", they are not the data — but stalling publication is.
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
				// Slow reader: this notice is dropped.
			}
		}
	}()
	return out, nil
}
