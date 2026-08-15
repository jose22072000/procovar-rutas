package events

import (
	"context"
	"os"
	"testing"
	"time"
)

// Tested against a real Redis: what matters here is exactly what an in-memory
// channel would hide — that one process publishes and ANOTHER receives, which is
// the real case (rutas-ingesta publishes, rutas-api serves the SSE).
func TestPublicarYRecibir(t *testing.T) {
	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		t.Skip("sin REDIS_URL_TEST")
	}

	publisher, err := New(url, "prueba-rutas:")
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()

	listener, err := New(url, "prueba-rutas:")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	ctx, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()

	channel, err := listener.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err := publisher.Publish(ctx, Event{Type: TypeFile, Detail: "procesado"}); err != nil {
		t.Fatal(err)
	}

	select {
	case e := <-channel:
		if e.Type != TypeFile || e.Detail != "procesado" {
			t.Fatalf("llegó otro evento: %+v", e)
		}
		if e.At.IsZero() {
			t.Fatal("el evento debería llevar la hora puesta")
		}
	case <-ctx.Done():
		t.Fatal("no llegó el evento")
	}
}

// The prefix really does separate: a PEDIDO event cannot show up in rutas.
func TestElPrefijoSepara(t *testing.T) {
	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		t.Skip("sin REDIS_URL_TEST")
	}

	rutas, _ := New(url, "prueba-rutas:")
	defer rutas.Close()
	otro, _ := New(url, "prueba-pedido:")
	defer otro.Close()

	ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelar()

	channel, err := rutas.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := otro.Publish(ctx, Event{Type: TypeQueue}); err != nil {
		t.Fatal(err)
	}

	select {
	case e := <-channel:
		t.Fatalf("se coló un evento de otra aplicación: %+v", e)
	case <-time.After(1 * time.Second):
		// Correcto: no llegó nada.
	}
}
