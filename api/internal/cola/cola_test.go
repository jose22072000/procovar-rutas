package cola

import (
	"context"
	"os"
	"testing"
	"time"
)

// Se salta sola si no hay Redis:
//
//	docker run -d --rm --name rutas-redis -p 56379:6379 redis:7-alpine
//	REDIS_URL_TEST=redis://127.0.0.1:56379/0 go test ./internal/cola/...
func nueva(t *testing.T) *Cola {
	t.Helper()
	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		t.Skip("sin REDIS_URL_TEST: se salta la prueba de la cola")
	}

	// Prefijo propio de la prueba: no se toca lo que haya en el Redis compartido.
	c, err := Nueva(url, "procovar-rutas-prueba:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("Redis no responde: %v", err)
	}
	c.rdb.Del(ctx, c.pendientes(), c.procesando(), c.fallidos())
	t.Cleanup(func() {
		c.rdb.Del(ctx, c.pendientes(), c.procesando(), c.fallidos())
		c.Cerrar()
	})
	return c
}

func trabajo(id string) Trabajo {
	return Trabajo{DriveFileID: id, Nombre: id + ".gpx", ContenidoBase64: "eA=="}
}

func TestEncolarYTomarEnOrden(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := c.Encolar(ctx, trabajo(id)); err != nil {
			t.Fatal(err)
		}
	}

	// FIFO: el primero que entra es el primero que sale.
	for _, esperado := range []string{"a", "b", "c"} {
		got, crudo, err := c.Tomar(ctx, time.Second)
		if err != nil || got == nil {
			t.Fatalf("tomar %s: %v", esperado, err)
		}
		if got.DriveFileID != esperado {
			t.Errorf("salió %s, se esperaba %s", got.DriveFileID, esperado)
		}
		if err := c.Terminar(ctx, crudo); err != nil {
			t.Fatal(err)
		}
	}

	e, _ := c.Estado(ctx)
	if e.Pendientes != 0 || e.Procesando != 0 {
		t.Errorf("estado = %+v; debería quedar vacía", e)
	}
}

// Lo importante de esta cola: un reinicio a mitad de proceso no puede perder el
// recorrido de un día.
func TestUnReinicioNoPierdeElTrabajo(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	if err := c.Encolar(ctx, trabajo("a")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Tomar(ctx, time.Second); err != nil {
		t.Fatal(err)
	}

	// Aquí el proceso "se cae": el trabajo se quedó en procesando.
	e, _ := c.Estado(ctx)
	if e.Procesando != 1 {
		t.Fatalf("procesando = %d", e.Procesando)
	}

	n, err := c.Recuperar(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recuperados = %d (%v)", n, err)
	}

	got, _, err := c.Tomar(ctx, time.Second)
	if err != nil || got == nil || got.DriveFileID != "a" {
		t.Errorf("el trabajo tenía que volver a la cola: %+v (%v)", got, err)
	}
}

func TestReintentaYAcabaApartando(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	if err := c.Encolar(ctx, trabajo("a")); err != nil {
		t.Fatal(err)
	}

	for intento := 1; intento <= MaxIntentos; intento++ {
		got, crudo, err := c.Tomar(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatalf("intento %d: la cola estaba vacía antes de tiempo", intento)
		}
		if err := c.Fallar(ctx, crudo, *got); err != nil {
			t.Fatal(err)
		}
	}

	e, _ := c.Estado(ctx)
	if e.Fallidos != 1 {
		t.Errorf("fallidos = %d; tras %d intentos debería apartarse", e.Fallidos, MaxIntentos)
	}
	if e.Pendientes != 0 {
		t.Errorf("pendientes = %d; no puede seguir reintentándose para siempre", e.Pendientes)
	}
}

func TestTomarDeUnaColaVaciaNoSeQuedaColgado(t *testing.T) {
	c := nueva(t)

	inicio := time.Now()
	got, _, err := c.Tomar(context.Background(), 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("no debería haber nada: %+v", got)
	}
	if time.Since(inicio) > 2*time.Second {
		t.Error("la espera debería respetar el tiempo pedido")
	}
}

// El prefijo es lo que impide pisar las claves de PEDIDO en el Redis compartido.
func TestElPrefijoAislaLasClaves(t *testing.T) {
	c, err := Nueva("redis://127.0.0.1:6379/0", "procovar-rutas")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Cerrar()

	if c.pendientes() != "procovar-rutas:ingesta:pendientes" {
		t.Errorf("clave = %q; falta el separador", c.pendientes())
	}

	porDefecto, _ := Nueva("redis://127.0.0.1:6379/0", "")
	defer porDefecto.Cerrar()
	if porDefecto.pendientes() != "procovar-rutas:ingesta:pendientes" {
		t.Errorf("clave por defecto = %q", porDefecto.pendientes())
	}
}
