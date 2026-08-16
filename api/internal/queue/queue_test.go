package queue

import (
	"context"
	"os"
	"testing"
	"time"
)

// Se salta sola si no hay Redis:
//
//	docker run -d --rm --name rutas-redis -p 56379:6379 redis:7-alpine
//	REDIS_URL_TEST=redis://127.0.0.1:56379/0 go test ./internal/queue/...
func nueva(t *testing.T) *Queue {
	t.Helper()
	url := os.Getenv("REDIS_URL_TEST")
	if url == "" {
		t.Skip("sin REDIS_URL_TEST: se salta la prueba de la cola")
	}

	// The test gets its own prefix: nothing in the shared Redis is touched.
	c, err := New(url, "procovar-rutas-prueba:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("Redis no responde: %v", err)
	}
	c.rdb.Del(ctx, c.pending(), c.processing(), c.failed())
	t.Cleanup(func() {
		c.rdb.Del(ctx, c.pending(), c.processing(), c.failed())
		c.Close()
	})
	return c
}

func trabajo(id string) Job {
	return Job{DriveFileID: id, Name: id + ".gpx", ContentBase64: "eA=="}
}

func TestEncolarYTomarEnOrden(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := c.Enqueue(ctx, trabajo(id)); err != nil {
			t.Fatal(err)
		}
	}

	// FIFO: first in, first out.
	for _, esperado := range []string{"a", "b", "c"} {
		got, crudo, err := c.Take(ctx, time.Second)
		if err != nil || got == nil {
			t.Fatalf("tomar %s: %v", esperado, err)
		}
		if got.DriveFileID != esperado {
			t.Errorf("salió %s, se esperaba %s", got.DriveFileID, esperado)
		}
		if err := c.Finish(ctx, crudo); err != nil {
			t.Fatal(err)
		}
	}

	e, _ := c.Stats(ctx)
	if e.Pending != 0 || e.Processing != 0 {
		t.Errorf("estado = %+v; debería quedar vacía", e)
	}
}

// What matters about this queue: a restart mid-processing cannot lose a day's
// route.
func TestUnReinicioNoPierdeElTrabajo(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	if err := c.Enqueue(ctx, trabajo("a")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Take(ctx, time.Second); err != nil {
		t.Fatal(err)
	}

	// Here the process "dies": the job was left in processing.
	e, _ := c.Stats(ctx)
	if e.Processing != 1 {
		t.Fatalf("processing = %d", e.Processing)
	}

	n, err := c.Recover(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recuperados = %d (%v)", n, err)
	}

	got, _, err := c.Take(ctx, time.Second)
	if err != nil || got == nil || got.DriveFileID != "a" {
		t.Errorf("el trabajo tenía que volver a la cola: %+v (%v)", got, err)
	}
}

func TestReintentaYAcabaApartando(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	if err := c.Enqueue(ctx, trabajo("a")); err != nil {
		t.Fatal(err)
	}

	for intento := 1; intento <= MaxAttempts; intento++ {
		got, crudo, err := c.Take(ctx, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatalf("intento %d: la cola estaba vacía antes de tiempo", intento)
		}
		if err := c.Fail(ctx, crudo, *got); err != nil {
			t.Fatal(err)
		}
	}

	e, _ := c.Stats(ctx)
	if e.Failed != 1 {
		t.Errorf("failed = %d; tras %d intentos debería apartarse", e.Failed, MaxAttempts)
	}
	if e.Pending != 0 {
		t.Errorf("pending = %d; no puede seguir reintentándose para siempre", e.Pending)
	}
}

func TestTomarDeUnaColaVaciaNoSeQuedaColgado(t *testing.T) {
	c := nueva(t)

	inicio := time.Now()
	got, _, err := c.Take(context.Background(), 300*time.Millisecond)
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

// The prefix is what keeps PEDIDO's keys safe in the shared Redis.
func TestElPrefijoAislaLasClaves(t *testing.T) {
	c, err := New("redis://127.0.0.1:6379/0", "procovar-rutas")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.pending() != "procovar-rutas:ingesta:pending" {
		t.Errorf("key = %q; falta el separador", c.pending())
	}

	porDefecto, _ := New("redis://127.0.0.1:6379/0", "")
	defer porDefecto.Close()
	if porDefecto.pending() != "procovar-rutas:ingesta:pending" {
		t.Errorf("key por defecto = %q", porDefecto.pending())
	}
}

// La cuenta dueña de la carpeta tiene que sobrevivir el viaje por Redis.
//
// Si no, se pierde justo entre el API y quien procesa: la ingesta identifica bien la
// sucursal, la manda, y el consumidor reconstruye el empuje sin ella. Pasó, y el
// síntoma era mudo — todo terminaba en una sucursal de relleno sin un solo error.
func TestLaCuentaSobreviveLaCola(t *testing.T) {
	c := nueva(t)
	ctx := context.Background()

	if err := c.Enqueue(ctx, Job{
		DriveFileID: "f-cuenta", Name: "20260812.gpx",
		Account: "Camagüey Procovar", FolderID: "carpeta-1",
		ContentBase64: "PGdweD48L2dweD4=",
	}); err != nil {
		t.Fatal(err)
	}

	job, crudo, err := c.Take(ctx, 2*time.Second)
	if err != nil || job == nil {
		t.Fatalf("no salió de la cola: %v", err)
	}
	if job.Account != "Camagüey Procovar" {
		t.Fatalf("la cuenta se perdió en la cola: %q", job.Account)
	}
	if job.FolderID != "carpeta-1" {
		t.Fatalf("la carpeta se perdió: %q", job.FolderID)
	}
	_ = c.Finish(ctx, crudo)
}
