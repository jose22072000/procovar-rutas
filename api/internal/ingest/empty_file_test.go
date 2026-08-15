package ingest_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Un .gpx de 0 bytes existe en Drive. Tiene que quedar REGISTRADO con su error,
// no tumbar la tanda entera de n8n ni desaparecer sin rastro.
func TestFicheroVacioQuedaRegistrado(t *testing.T) {
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("sin DATABASE_URL_TEST")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	q := store.New(pool)

	svc := ingest.NewService(pool, ingest.SingleAccount(nil), nil, 100)
	_, _, err = svc.Receive(ctx, ingest.Pushed{
		Account: "camaguey.procovar@gmail.com", FolderID: "carpeta-vacia",
		DriveFileID: "f-vacio", Name: "20260813.gpx",
		FolderPath: []string{"GPS Prueba"}, Content: nil,
	})
	if err != nil {
		t.Fatalf("un fichero vacío no puede fallar: %v", err)
	}

	f, err := q.FileByDriveID(ctx, "f-vacio")
	if err != nil {
		t.Fatalf("el fichero vacío no quedó registrado: %v", err)
	}
	if f.Status == store.FileProcessed {
		t.Fatalf("un fichero vacío no puede quedar como procesado")
	}
	t.Logf("registrado con estado %s", f.Status)
}
