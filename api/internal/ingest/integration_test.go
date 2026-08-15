package ingest_test

// Prueba de extremo a extremo de la ingesta contra un Postgres de verdad, con
// un Drive falso: listar → descargar → parsear → resolver → guardar → recalcular
// el día. Es la que demuestra que las piezas encajan; las demás pruebas solo
// miran cada pieza por separado.
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

	// Cada prueba parte de cero. TRUNCATE ... CASCADE en vez de borrar la base:
	// las migraciones ya están aplicadas y volver a aplicarlas en cada prueba
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
		ID: "a1", Alias: gpx.Normalizar("Alexander"), OriginalAlias: "Alexander",
		SellerID: "t-alex", BranchID: ptr("s1"),
	}); err != nil {
		t.Fatalf("alias: %v", err)
	}
}

// rutaGpx genera una jornada que avanza: 09:00 a 16:00, hora de Cuba.
func rutaGpx(pasoMin int, moverse bool) []byte {
	cuerpo := ""
	i := 0
	for min := 9 * 60; min <= 16*60; min += pasoMin {
		lat, lon := 21.38, -77.91
		if moverse {
			lon += float64(i) * 0.01
		} else if i%2 == 0 {
			lat += 0.00007 // temblor del GPS estando quieto
		}
		// +4 h para pasar de hora local de Cuba a UTC en agosto.
		cuerpo += fmt.Sprintf(
			`<trkpt lat="%.5f" lon="%.5f"><time>2026-08-10T%02d:%02d:00Z</time></trkpt>`,
			lat, lon, (min/60)+4, min%60)
		i++
	}
	return gpxConNombre("ALEXANDER", cuerpo)
}

// gpxConNombre permite variar el nombre de la pista, que es una de las pistas
// que usa la resolución: los loggers suelen meter ahí el perfil.
func gpxConNombre(nombre, cuerpo string) []byte {
	return []byte(`<?xml version="1.0"?><gpx version="1.1" creator="GPS Logger">
<trk><name>` + nombre + `</name><trkseg>` + cuerpo + `</trkseg></trk></gpx>`)
}

// rutaAnonima es la misma jornada pero sin el nombre del vendedor dentro del
// file: el caso de la tableta, donde solo la carpeta puede decir de quién es.
func rutaAnonima(pasoMin int) []byte {
	completo := string(rutaGpx(pasoMin, true))
	ini := strings.Index(completo, "<trkseg>") + len("<trkseg>")
	fin := strings.Index(completo, "</trkseg>")
	return gpxConNombre("TAB-CMG-04", completo[ini:fin])
}

func servicio(t *testing.T, pool *pgxpool.Pool, d drive.Cliente) *ingest.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ingest.NewService(pool, ingest.SingleAccount(d), log, 100)
}

func TestIngestaCompleta(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, rutaGpx(5, true))

	svc := servicio(t, pool, falso)
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

	// Y el día quedó calculado, que es lo que pinta el calendar.
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

// El repaso nocturno relista carpetas enteras: si volviera a descargar lo que ya
// tiene, cada noche bajaría miles de ficheros para nada.
func TestNoVuelveADescargarLoQueYaTiene(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, rutaGpx(15, true))

	svc := servicio(t, pool, falso)
	if _, err := svc.Scan(ctx, ingest.TypeNightly); err != nil {
		t.Fatal(err)
	}
	descargasPrimera := falso.Llamadas

	res, err := svc.Scan(ctx, ingest.TypeNightly)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 0 {
		t.Errorf("segunda pasada: nuevos = %d, se esperaba 0", res.New)
	}
	if falso.Llamadas != descargasPrimera {
		t.Errorf("descargas = %d, se esperaban %d: no debe volver a bajarlo",
			falso.Llamadas, descargasPrimera)
	}
}

// Un file que no se puede leer no puede tumbar el barrido de los demás.
func TestUnFicheroMaloNoTumbaElResto(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{ID: "d0", Name: "roto.gpx"}, []byte("no es xml"))
	falso.Agregar(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, rutaGpx(15, true))

	svc := servicio(t, pool, falso)
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

// El caso que da sentido al proyecto, de punta a punta.
func TestDiaSinMovimientoLlegaAlCalendario(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, rutaGpx(5, false))

	svc := servicio(t, pool, falso)
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

// Sin file, el vendedor tiene que aparecer igualmente en el calendario: una
// ausencia que no es una fila no se puede contar ni ordenar.
func TestAusenciaApareceComoFila(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	svc := servicio(t, pool, drive.NuevoFalso())
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

// Marcar ausencias no puede pisar un día que ya tiene datos.
func TestAusenciasNoPisanUnDiaConDatos(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{ID: "d1", Name: "alexander_2026-08-10.gpx"}, rutaGpx(15, true))
	svc := servicio(t, pool, falso)
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

// La estructura real de Procovar: una cuenta de Google por sucursal, dentro una
// carpeta por perfil de GPS —con el nombre del vendedor o de la tableta— y
// dentro los ficheros AAAAMMDD.gpx, que NO llevan el nombre de nadie.
//
// El vendedor solo se puede deducir de la carpeta: si esta prueba falla, la
// ingesta real deja todos los ficheros en la bandeja.
func TestEstructuraRealCarpetaPorPerfilDeGps(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{
		ID:         "d1",
		Name:       "20260810.gpx",
		FolderPath: []string{"Alexander"}, // el perfil del GPS
	}, rutaGpx(10, true))

	svc := servicio(t, pool, falso)
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

// Y si el perfil del GPS es el nombre de la tableta, no el del vendedor, el
// file cae en la bandeja con la pista para casarlo UNA vez.
func TestPerfilConNombreDeTabletVaALaBandeja(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.File{
		ID: "d1", Name: "20260810.gpx", FolderPath: []string{"TAB-CMG-04"},
	}, rutaAnonima(10))

	svc := servicio(t, pool, falso)
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

	// Casado el alias, el siguiente file de esa tableta se resuelve solo.
	if _, err := q.CreateAlias(ctx, store.CreateAliasParams{
		ID: "a2", Alias: gpx.Normalizar("TAB-CMG-04"), OriginalAlias: "TAB-CMG-04",
		SellerID: "t-alex", BranchID: ptr("s1"),
	}); err != nil {
		t.Fatal(err)
	}
	falso.Agregar(carpeta, drive.File{
		ID: "d2", Name: "20260811.gpx", FolderPath: []string{"TAB-CMG-04"},
	}, gpxConNombre("TAB-CMG-04", `<trkpt lat="21.38" lon="-77.91"><time>2026-08-11T13:00:00Z</time></trkpt>`))

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

// Sin credenciales de Google el barrido no puede hacer nada, pero tampoco puede
// tumbar el proceso: la entrada por n8n sigue funcionando.
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
