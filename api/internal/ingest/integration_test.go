package ingest_test

// End-to-end test of ingest against a real Postgres, with a fake Drive: list →
// download → parse → resolve → store → recompute the day. It is the one that
// proves the pieces fit together; the other tests only look at each piece on its
// own.
//
// Se salta sola si no hay base:
//
//	docker run -d --rm --name rutas-pg -e POSTGRES_PASSWORD=prueba -p 55432:5432 postgres:16-alpine
//	DATABASE_URL_TEST='postgres://postgres:prueba@127.0.0.1:55432/postgres?sslmode=disable' go test ./...

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

const carpeta = "carpeta-camaguey"

func base(t *testing.T) (*pgxpool.Pool, *store.Queries) {
	t.Helper()
	url := os.Getenv("DATABASE_URL_TEST")
	if url == "" {
		t.Skip("sin DATABASE_URL_TEST: se salta la prueba de integración")
	}

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("conectando: %v", err)
	}
	t.Cleanup(pool.Close)

	// Every test starts from zero. TRUNCATE ... CASCADE rather than dropping the
	// database: the migrations are already applied and re-applying them per test
	// sería mucho más lento.
	_, err = pool.Exec(context.Background(),
		`TRUNCATE track_point, stop, track_day, gpx_file, device_alias, import_log,
		 drive_source, supervision, trabajador, sucursal_config, feriado, sucursal CASCADE`)
	if err != nil {
		t.Fatalf("limpiando: %v", err)
	}

	return pool, store.New(pool)
}

func semilla(t *testing.T, q *store.Queries) {
	t.Helper()
	ctx := context.Background()

	if _, err := q.CreateTestBranch(ctx, store.CreateTestBranchParams{
		ID: "s1", Name: "Camagüey",
	}); err != nil {
		t.Fatalf("sucursal: %v", err)
	}
	if _, err := q.CreateTestSeller(ctx, store.CreateTestSellerParams{
		ID: "t-alex", Name: "Alexander", BranchID: "s1",
	}); err != nil {
		t.Fatalf("trabajador: %v", err)
	}
	if _, err := q.CreateSource(ctx, store.CreateSourceParams{
		ID: "f1", Name: "Rutas Camagüey", FolderID: carpeta,
		Type: store.SourceBranch, BranchID: ptr("s1"),
	}); err != nil {
		t.Fatalf("source: %v", err)
	}
	if _, err := q.CreateAlias(ctx, store.CreateAliasParams{
		ID: "a1", Alias: gpx.Normalize("Alexander"), OriginalAlias: "Alexander",
		SellerID: "t-alex", BranchID: ptr("s1"),
	}); err != nil {
		t.Fatalf("alias: %v", err)
	}
}

// gpxRoute generates a workday that advances: 09:00 to 16:00, Cuban time.
func gpxRoute(pasoMin int, moverse bool) []byte {
	cuerpo := ""
	i := 0
	for min := 9 * 60; min <= 16*60; min += pasoMin {
		lat, lon := 21.38, -77.91
		if moverse {
			lon += float64(i) * 0.01
		} else if i%2 == 0 {
			lat += 0.00007 // temblor del GPS estando quieto
		}
		// +4 h to go from Cuban local time to UTC in August.
		cuerpo += fmt.Sprintf(
			`<trkpt lat="%.5f" lon="%.5f"><time>2026-08-10T%02d:%02d:00Z</time></trkpt>`,
			lat, lon, (min/60)+4, min%60)
		i++
	}
	return gpxWithName("ALEXANDER", cuerpo)
}

// gpxWithName allows varying the track name, one of the hints resolution uses:
// loggers usually put the profile in there.
func gpxWithName(nombre, cuerpo string) []byte {
	return []byte(`<?xml version="1.0"?><gpx version="1.1" creator="GPS Logger">
<trk><name>` + nombre + `</name><trkseg>` + cuerpo + `</trkseg></trk></gpx>`)
}

// anonymousRoute is the same workday but without the seller's name inside the
// file: the tablet case, where only the folder can say whose it is.
func anonymousRoute(pasoMin int) []byte {
	completo := string(gpxRoute(pasoMin, true))
	ini := strings.Index(completo, "<trkseg>") + len("<trkseg>")
	fin := strings.Index(completo, "</trkseg>")
	return gpxWithName("TAB-CMG-04", completo[ini:fin])
}

func servicio(t *testing.T, pool *pgxpool.Pool, d drive.Client) *ingest.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ingest.NewService(pool, ingest.SingleAccount(d), log, 100)
}

func TestIngestaCompleta(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, gpxRoute(5, true))

	svc := servicio(t, pool, fake)
	res, err := svc.Scan(ctx, ingest.TypeIncremental)
	if err != nil {
		t.Fatalf("barrido: %v", err)
	}
	if res.New != 1 || res.Failed != 0 {
		t.Fatalf("resumen = %+v", res)
	}
	if res.Points == 0 {
		t.Fatal("no se insertó ningún punto")
	}

	file, err := q.FileByDriveID(ctx, "d1")
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if file.Status != store.FileProcessed {
		t.Errorf("status = %s (%v)", file.Status, file.Error)
	}
	if file.SellerID == nil || *file.SellerID != "t-alex" {
		t.Errorf("no se resolvió el vendedor: %v", file.SellerID)
	}

	// And the day came out computed, which is what the calendar paints.
	dias, err := q.Calendar(ctx, store.CalendarParams{
		FromDate: dia("2026-08-10"), ToDate: dia("2026-08-10"), Sellers: []string{},
	})
	if err != nil {
		t.Fatalf("calendario: %v", err)
	}
	if len(dias) != 1 {
		t.Fatalf("días = %d", len(dias))
	}
	if dias[0].Status != store.DayOk {
		t.Errorf("status del día = %s, se esperaba OK", dias[0].Status)
	}
	if dias[0].NetKm < 5 {
		t.Errorf("km = %v", dias[0].NetKm)
	}
}

// The nightly sweep relists whole folders: if it downloaded again what it already
// has, every night it would fetch thousands of files for nothing.
func TestNoVuelveADescargarLoQueYaTiene(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, gpxRoute(15, true))

	svc := servicio(t, pool, fake)
	if _, err := svc.Scan(ctx, ingest.TypeNightly); err != nil {
		t.Fatal(err)
	}
	descargasPrimera := fake.Calls

	res, err := svc.Scan(ctx, ingest.TypeNightly)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 0 {
		t.Errorf("segunda pasada: nuevos = %d, se esperaba 0", res.New)
	}
	if fake.Calls != descargasPrimera {
		t.Errorf("descargas = %d, se esperaban %d: no debe volver a bajarlo",
			fake.Calls, descargasPrimera)
	}
}

// A file that cannot be read must not bring down everyone else's scan.
func TestUnFicheroMaloNoTumbaElResto(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{ID: "d0", Name: "roto.gpx"}, []byte("no es xml"))
	fake.Add(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, gpxRoute(15, true))

	svc := servicio(t, pool, fake)
	res, err := svc.Scan(ctx, ingest.TypeIncremental)
	if err != nil {
		t.Fatalf("barrido: %v", err)
	}
	if res.New != 2 {
		t.Errorf("nuevos = %d: el roto también se registra, para que salga en la bandeja", res.New)
	}

	roto, err := q.FileByDriveID(ctx, "d0")
	if err != nil {
		t.Fatalf("el file roto no se registró: %v", err)
	}
	if roto.Status != store.FileFailed || roto.Error == nil {
		t.Errorf("= %s / %v", roto.Status, roto.Error)
	}

	bueno, err := q.FileByDriveID(ctx, "d1")
	if err != nil || bueno.Status != store.FileProcessed {
		t.Errorf("el file bueno debería haberse procesado igual: %v", err)
	}
}

// The case that gives the project its point, end to end.
func TestDiaSinMovimientoLlegaAlCalendario(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, gpxRoute(5, false))

	svc := servicio(t, pool, fake)
	if _, err := svc.Scan(ctx, ingest.TypeIncremental); err != nil {
		t.Fatal(err)
	}

	dias, err := q.Calendar(ctx, store.CalendarParams{
		FromDate: dia("2026-08-10"), ToDate: dia("2026-08-10"), Sellers: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dias) != 1 || dias[0].Status != store.DayNoMovement {
		t.Fatalf("= %+v; se esperaba SIN_MOVIMIENTO", dias)
	}
	if dias[0].SpreadM == nil || *dias[0].SpreadM >= 300 {
		t.Errorf("radio = %v", dias[0].SpreadM)
	}
}

// With no file, the seller still has to appear on the calendar: an
// absence that is not a row cannot be counted or sorted.
func TestAusenciaApareceComoFila(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	svc := servicio(t, pool, drive.NewFake())
	n, err := svc.MarkAbsences(ctx, dia("2026-08-10"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ausencias = %d", n)
	}

	dias, err := q.Calendar(ctx, store.CalendarParams{
		FromDate: dia("2026-08-10"), ToDate: dia("2026-08-10"), Sellers: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dias) != 1 || dias[0].Status != store.DayNoFile {
		t.Fatalf("= %+v", dias)
	}
}

// Marking absences must not overwrite a day that already has data.
func TestAusenciasNoPisanUnDiaConDatos(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, gpxRoute(15, true))
	svc := servicio(t, pool, fake)
	if _, err := svc.Scan(ctx, ingest.TypeIncremental); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MarkAbsences(ctx, dia("2026-08-10")); err != nil {
		t.Fatal(err)
	}

	dias, _ := q.Calendar(ctx, store.CalendarParams{
		FromDate: dia("2026-08-10"), ToDate: dia("2026-08-10"), Sellers: []string{},
	})
	if len(dias) != 1 || dias[0].Status != store.DayOk {
		t.Fatalf("= %+v; el día trabajado no puede volverse una ausencia", dias)
	}
}

func dia(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func ptr[T any](v T) *T { return &v }

// Procovar's real structure: one Google account per branch, inside it one folder
// per GPS profile — named after the seller or the tablet — and inside that the
// YYYYMMDD.gpx files, which carry NOBODY's name.
//
// The seller can only be inferred from the folder: if this test fails, the real
// ingest leaves every file in the inbox.
func TestEstructuraRealCarpetaPorPerfilDeGps(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{
		ID:         "d1",
		Name:       "20260810.gpx",
		FolderPath: []string{"Alexander"}, // el perfil del GPS
	}, gpxRoute(10, true))

	svc := servicio(t, pool, fake)
	if _, err := svc.Scan(ctx, ingest.TypeIncremental); err != nil {
		t.Fatal(err)
	}

	f, err := q.FileByDriveID(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if f.SellerID == nil || *f.SellerID != "t-alex" {
		t.Fatalf("el vendedor tenía que salir de la carpeta: %v", f.SellerID)
	}
	if f.Date == nil || f.Date.Format("2006-01-02") != "2026-08-10" {
		t.Errorf("date = %v; el nombre AAAAMMDD debería bastar", f.Date)
	}
	if f.Status != store.FileProcessed {
		t.Errorf("status = %s", f.Status)
	}
}

// And if the GPS profile is the tablet's name rather than the seller's, the file
// lands in the inbox with the hint to match it ONCE.
func TestPerfilConNombreDeTabletVaALaBandeja(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	fake := drive.NewFake()
	fake.Add(carpeta, drive.File{
		ID: "d1", Name: "20260810.gpx", FolderPath: []string{"TAB-CMG-04"},
	}, anonymousRoute(10))

	svc := servicio(t, pool, fake)
	if _, err := svc.Scan(ctx, ingest.TypeIncremental); err != nil {
		t.Fatal(err)
	}

	f, err := q.FileByDriveID(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != store.FileUnassigned {
		t.Fatalf("status = %s, se esperaba SIN_ASIGNAR", f.Status)
	}
	if f.AliasHint == nil || *f.AliasHint != "TAB-CMG-04" {
		t.Errorf("pista = %v; es lo que el admin casa con el vendedor", f.AliasHint)
	}

	// Once the alias is matched, the next file from that tablet resolves on its own.
	if _, err := q.CreateAlias(ctx, store.CreateAliasParams{
		ID: "a2", Alias: gpx.Normalize("TAB-CMG-04"), OriginalAlias: "TAB-CMG-04",
		SellerID: "t-alex", BranchID: ptr("s1"),
	}); err != nil {
		t.Fatal(err)
	}
	fake.Add(carpeta, drive.File{
		ID: "d2", Name: "20260811.gpx", FolderPath: []string{"TAB-CMG-04"},
	}, gpxWithName("TAB-CMG-04", `<trkpt lat="21.38" lon="-77.91"><time>2026-08-11T13:00:00Z</time></trkpt>`))

	if _, err := svc.Scan(ctx, ingest.TypeNightly); err != nil {
		t.Fatal(err)
	}
	f2, err := q.FileByDriveID(ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	if f2.SellerID == nil || *f2.SellerID != "t-alex" {
		t.Errorf("tras casar el alias ya no debería preguntar: %v", f2.SellerID)
	}
}

// Without Google credentials the scan can do nothing, but it must not bring down
// the process either: input through n8n keeps working.
func TestSinAccesoADriveNoRevienta(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)

	svc := servicio(t, pool, nil) // sin cliente de Drive
	res, err := svc.Scan(context.Background(), ingest.TypeIncremental)
	if err != nil {
		t.Fatalf("Scan no debe devolver error global: %v", err)
	}
	if res.New != 0 {
		t.Errorf("nuevos = %d", res.New)
	}
}
