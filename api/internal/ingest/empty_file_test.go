package ingest_test

import (
	"context"
	"log/slog"
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

// Un fichero ya visto no vuelve a entrar, y —lo que importa— NO se resuelve su
// carpeta: al archivarlo en Drive queda dentro de "Procesados", y si eso llegara a
// la resolución se daría de alta una fuente, y un "vendedor", con ese nombre.
func TestFicheroYaVistoNoCreaCarpetaDeArchivo(t *testing.T) {
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

	entra := ingest.Pushed{
		Account: "camaguey.procovar@gmail.com", FolderID: "carpeta-perfil",
		DriveFileID: "f-repe", Name: "20260812.gpx",
		FolderPath: []string{"GPS Prueba"}, Content: []byte("<gpx></gpx>"),
	}
	if _, _, err := svc.Receive(ctx, entra); err != nil {
		t.Fatalf("primera entrada: %v", err)
	}

	// El mismo fichero, ya archivado: llega con la carpeta de archivo como padre.
	otra := entra
	otra.FolderID = "carpeta-procesados"
	otra.FolderPath = []string{"Procesados"}
	if _, _, err := svc.Receive(ctx, otra); err != nil {
		t.Fatalf("el reenvío de un fichero archivado no debe fallar: %v", err)
	}

	fuentes, err := q.ActiveSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fuentes {
		if f.Name == "Procesados" || f.FolderID == "carpeta-procesados" {
			t.Fatalf("se dio de alta la carpeta de archivo como fuente: %+v", f.Name)
		}
	}
}

// La carpeta aprende de qué sucursal es en cuanto llega un empuje con cuenta, y se
// lleva consigo lo que ya había entrado por ella. Es el caso real: las 53 carpetas
// entraron por migración con una credencial de relleno y todo cayó en "principal".
func TestLaCarpetaAprendeSuSucursalYSeLlevaLoSuyo(t *testing.T) {
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
	svc := ingest.NewService(pool, ingest.SingleAccount(nil), slog.New(slog.NewTextHandler(os.Stderr, nil)), 100)

	// Primero entra sin decir de qué cuenta viene: cae en la sucursal de relleno.
	sinCuenta := ingest.Pushed{
		FolderID: "carpeta-cam", DriveFileID: "f-1", Name: "20260810.gpx",
		FolderPath: []string{"CAM2"}, Content: []byte("<gpx></gpx>"),
		Account: "principal",
	}
	if _, _, err := svc.Receive(ctx, sinCuenta); err != nil {
		t.Fatalf("primera entrada: %v", err)
	}

	// Ahora llega otro fichero de la MISMA carpeta, ya con su cuenta de verdad.
	conCuenta := sinCuenta
	conCuenta.DriveFileID = "f-2"
	conCuenta.Account = "camaguey.procovar@gmail.com"
	if _, _, err := svc.Receive(ctx, conCuenta); err != nil {
		t.Fatalf("segunda entrada: %v", err)
	}

	suc, err := q.BranchByName(ctx, "camaguey")
	if err != nil {
		t.Fatalf("no se creó la sucursal camaguey: %v", err)
	}

	// Y el fichero que había entrado antes tiene que haberse mudado con ella.
	f, err := q.FileByDriveID(ctx, "f-1")
	if err != nil {
		t.Fatal(err)
	}
	if f.BranchID == nil || *f.BranchID != suc.ID {
		t.Fatalf("el fichero anterior se quedó en la sucursal vieja: %v", f.BranchID)
	}
	v, err := q.SellerByNameInBranch(ctx, store.SellerByNameInBranchParams{Name: "CAM2", BranchID: suc.ID})
	if err != nil {
		t.Fatalf("el vendedor no se mudó a camaguey: %v", err)
	}
	t.Logf("vendedor %q en sucursal camaguey", v.Name)
}
