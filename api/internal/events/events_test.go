package events

import (
	"context"
	"os"
	"testing"
	"time"
)

// Prueba contra un Redis de verdad: lo que importa aquí es justo lo que no se ve
// en memoria — que un proceso publique y OTRO lo reciba, que es el caso real
// (rutas-ingesta publica, rutas-api sirve el SSE).
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

// El prefix separa de verdad: un evento de PEDIDO no puede aparecer en rutas.
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
