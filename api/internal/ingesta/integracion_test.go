package ingesta_test

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

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/ingesta"
)

const carpeta = "carpeta-camaguey"

func base(t *testing.T) (*pgxpool.Pool, *almacen.Queries) {
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

	return pool, almacen.New(pool)
}

func semilla(t *testing.T, q *almacen.Queries) {
	t.Helper()
	ctx := context.Background()

	if _, err := q.CrearSucursalDePrueba(ctx, almacen.CrearSucursalDePruebaParams{
		ID: "s1", Nombre: "Camagüey",
	}); err != nil {
		t.Fatalf("sucursal: %v", err)
	}
	if _, err := q.CrearTrabajadorDePrueba(ctx, almacen.CrearTrabajadorDePruebaParams{
		ID: "t-alex", Nombre: "Alexander", SucursalID: "s1",
	}); err != nil {
		t.Fatalf("trabajador: %v", err)
	}
	if _, err := q.CrearFuente(ctx, almacen.CrearFuenteParams{
		ID: "f1", Nombre: "Rutas Camagüey", FolderID: carpeta,
		Tipo: almacen.TipoFuenteSUCURSAL, SucursalID: ptr("s1"),
	}); err != nil {
		t.Fatalf("fuente: %v", err)
	}
	if _, err := q.CrearAlias(ctx, almacen.CrearAliasParams{
		ID: "a1", Alias: gpx.Normalizar("Alexander"), AliasOriginal: "Alexander",
		TrabajadorID: "t-alex", SucursalID: ptr("s1"),
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
// fichero: el caso de la tableta, donde solo la carpeta puede decir de quién es.
func rutaAnonima(pasoMin int) []byte {
	completo := string(rutaGpx(pasoMin, true))
	ini := strings.Index(completo, "<trkseg>") + len("<trkseg>")
	fin := strings.Index(completo, "</trkseg>")
	return gpxConNombre("TAB-CMG-04", completo[ini:fin])
}

func servicio(t *testing.T, pool *pgxpool.Pool, d drive.Cliente) *ingesta.Servicio {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ingesta.NuevoServicio(pool, ingesta.UnaCuenta(d), log, 100)
}

func TestIngestaCompleta(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}, rutaGpx(5, true))

	svc := servicio(t, pool, falso)
	res, err := svc.Barrer(ctx, ingesta.TipoIncremental)
	if err != nil {
		t.Fatalf("barrido: %v", err)
	}
	if res.Nuevos != 1 || res.Errores != 0 {
		t.Fatalf("resumen = %+v", res)
	}
	if res.Puntos == 0 {
		t.Fatal("no se insertó ningún punto")
	}

	fichero, err := q.FicheroPorDriveID(ctx, "d1")
	if err != nil {
		t.Fatalf("fichero: %v", err)
	}
	if fichero.Estado != almacen.EstadoFicheroPROCESADO {
		t.Errorf("estado = %s (%v)", fichero.Estado, fichero.Error)
	}
	if fichero.TrabajadorID == nil || *fichero.TrabajadorID != "t-alex" {
		t.Errorf("no se resolvió el vendedor: %v", fichero.TrabajadorID)
	}

	// Y el día quedó calculado, que es lo que pinta el calendario.
	dias, err := q.Calendario(ctx, almacen.CalendarioParams{
		Desde: dia("2026-08-10"), Hasta: dia("2026-08-10"), Trabajadores: []string{},
	})
	if err != nil {
		t.Fatalf("calendario: %v", err)
	}
	if len(dias) != 1 {
		t.Fatalf("días = %d", len(dias))
	}
	if dias[0].Estado != almacen.EstadoDiaOK {
		t.Errorf("estado del día = %s, se esperaba OK", dias[0].Estado)
	}
	if dias[0].KmNetos < 5 {
		t.Errorf("km = %v", dias[0].KmNetos)
	}
}

// El repaso nocturno relista carpetas enteras: si volviera a descargar lo que ya
// tiene, cada noche bajaría miles de ficheros para nada.
func TestNoVuelveADescargarLoQueYaTiene(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}, rutaGpx(15, true))

	svc := servicio(t, pool, falso)
	if _, err := svc.Barrer(ctx, ingesta.TipoNocturno); err != nil {
		t.Fatal(err)
	}
	descargasPrimera := falso.Llamadas

	res, err := svc.Barrer(ctx, ingesta.TipoNocturno)
	if err != nil {
		t.Fatal(err)
	}
	if res.Nuevos != 0 {
		t.Errorf("segunda pasada: nuevos = %d, se esperaba 0", res.Nuevos)
	}
	if falso.Llamadas != descargasPrimera {
		t.Errorf("descargas = %d, se esperaban %d: no debe volver a bajarlo",
			falso.Llamadas, descargasPrimera)
	}
}

// Un fichero que no se puede leer no puede tumbar el barrido de los demás.
func TestUnFicheroMaloNoTumbaElResto(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.Fichero{ID: "d0", Nombre: "roto.gpx"}, []byte("no es xml"))
	falso.Agregar(carpeta, drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}, rutaGpx(15, true))

	svc := servicio(t, pool, falso)
	res, err := svc.Barrer(ctx, ingesta.TipoIncremental)
	if err != nil {
		t.Fatalf("barrido: %v", err)
	}
	if res.Nuevos != 2 {
		t.Errorf("nuevos = %d: el roto también se registra, para que salga en la bandeja", res.Nuevos)
	}

	roto, err := q.FicheroPorDriveID(ctx, "d0")
	if err != nil {
		t.Fatalf("el fichero roto no se registró: %v", err)
	}
	if roto.Estado != almacen.EstadoFicheroERROR || roto.Error == nil {
		t.Errorf("= %s / %v", roto.Estado, roto.Error)
	}

	bueno, err := q.FicheroPorDriveID(ctx, "d1")
	if err != nil || bueno.Estado != almacen.EstadoFicheroPROCESADO {
		t.Errorf("el fichero bueno debería haberse procesado igual: %v", err)
	}
}

// El caso que da sentido al proyecto, de punta a punta.
func TestDiaSinMovimientoLlegaAlCalendario(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}, rutaGpx(5, false))

	svc := servicio(t, pool, falso)
	if _, err := svc.Barrer(ctx, ingesta.TipoIncremental); err != nil {
		t.Fatal(err)
	}

	dias, err := q.Calendario(ctx, almacen.CalendarioParams{
		Desde: dia("2026-08-10"), Hasta: dia("2026-08-10"), Trabajadores: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dias) != 1 || dias[0].Estado != almacen.EstadoDiaSINMOVIMIENTO {
		t.Fatalf("= %+v; se esperaba SIN_MOVIMIENTO", dias)
	}
	if dias[0].RadioDispersion == nil || *dias[0].RadioDispersion >= 300 {
		t.Errorf("radio = %v", dias[0].RadioDispersion)
	}
}

// Sin fichero, el vendedor tiene que aparecer igualmente en el calendario: una
// ausencia que no es una fila no se puede contar ni ordenar.
func TestAusenciaApareceComoFila(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	svc := servicio(t, pool, drive.NuevoFalso())
	n, err := svc.MarcarAusencias(ctx, dia("2026-08-10"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ausencias = %d", n)
	}

	dias, err := q.Calendario(ctx, almacen.CalendarioParams{
		Desde: dia("2026-08-10"), Hasta: dia("2026-08-10"), Trabajadores: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dias) != 1 || dias[0].Estado != almacen.EstadoDiaSINFICHERO {
		t.Fatalf("= %+v", dias)
	}
}

// Marcar ausencias no puede pisar un día que ya tiene datos.
func TestAusenciasNoPisanUnDiaConDatos(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.Fichero{ID: "d1", Nombre: "alexander_2026-08-10.gpx"}, rutaGpx(15, true))
	svc := servicio(t, pool, falso)
	if _, err := svc.Barrer(ctx, ingesta.TipoIncremental); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MarcarAusencias(ctx, dia("2026-08-10")); err != nil {
		t.Fatal(err)
	}

	dias, _ := q.Calendario(ctx, almacen.CalendarioParams{
		Desde: dia("2026-08-10"), Hasta: dia("2026-08-10"), Trabajadores: []string{},
	})
	if len(dias) != 1 || dias[0].Estado != almacen.EstadoDiaOK {
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
	falso.Agregar(carpeta, drive.Fichero{
		ID:          "d1",
		Nombre:      "20260810.gpx",
		RutaCarpeta: []string{"Alexander"}, // el perfil del GPS
	}, rutaGpx(10, true))

	svc := servicio(t, pool, falso)
	if _, err := svc.Barrer(ctx, ingesta.TipoIncremental); err != nil {
		t.Fatal(err)
	}

	f, err := q.FicheroPorDriveID(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if f.TrabajadorID == nil || *f.TrabajadorID != "t-alex" {
		t.Fatalf("el vendedor tenía que salir de la carpeta: %v", f.TrabajadorID)
	}
	if f.Fecha == nil || f.Fecha.Format("2006-01-02") != "2026-08-10" {
		t.Errorf("fecha = %v; el nombre AAAAMMDD debería bastar", f.Fecha)
	}
	if f.Estado != almacen.EstadoFicheroPROCESADO {
		t.Errorf("estado = %s", f.Estado)
	}
}

// Y si el perfil del GPS es el nombre de la tableta, no el del vendedor, el
// fichero cae en la bandeja con la pista para casarlo UNA vez.
func TestPerfilConNombreDeTabletVaALaBandeja(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)
	ctx := context.Background()

	falso := drive.NuevoFalso()
	falso.Agregar(carpeta, drive.Fichero{
		ID: "d1", Nombre: "20260810.gpx", RutaCarpeta: []string{"TAB-CMG-04"},
	}, rutaAnonima(10))

	svc := servicio(t, pool, falso)
	if _, err := svc.Barrer(ctx, ingesta.TipoIncremental); err != nil {
		t.Fatal(err)
	}

	f, err := q.FicheroPorDriveID(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if f.Estado != almacen.EstadoFicheroSINASIGNAR {
		t.Fatalf("estado = %s, se esperaba SIN_ASIGNAR", f.Estado)
	}
	if f.PistaAlias == nil || *f.PistaAlias != "TAB-CMG-04" {
		t.Errorf("pista = %v; es lo que el admin casa con el vendedor", f.PistaAlias)
	}

	// Casado el alias, el siguiente fichero de esa tableta se resuelve solo.
	if _, err := q.CrearAlias(ctx, almacen.CrearAliasParams{
		ID: "a2", Alias: gpx.Normalizar("TAB-CMG-04"), AliasOriginal: "TAB-CMG-04",
		TrabajadorID: "t-alex", SucursalID: ptr("s1"),
	}); err != nil {
		t.Fatal(err)
	}
	falso.Agregar(carpeta, drive.Fichero{
		ID: "d2", Nombre: "20260811.gpx", RutaCarpeta: []string{"TAB-CMG-04"},
	}, gpxConNombre("TAB-CMG-04", `<trkpt lat="21.38" lon="-77.91"><time>2026-08-11T13:00:00Z</time></trkpt>`))

	if _, err := svc.Barrer(ctx, ingesta.TipoNocturno); err != nil {
		t.Fatal(err)
	}
	f2, err := q.FicheroPorDriveID(ctx, "d2")
	if err != nil {
		t.Fatal(err)
	}
	if f2.TrabajadorID == nil || *f2.TrabajadorID != "t-alex" {
		t.Errorf("tras casar el alias ya no debería preguntar: %v", f2.TrabajadorID)
	}
}

// Sin credenciales de Google el barrido no puede hacer nada, pero tampoco puede
// tumbar el proceso: la entrada por n8n sigue funcionando.
func TestSinAccesoADriveNoRevienta(t *testing.T) {
	pool, q := base(t)
	semilla(t, q)

	svc := servicio(t, pool, nil) // sin cliente de Drive
	res, err := svc.Barrer(context.Background(), ingesta.TipoIncremental)
	if err != nil {
		t.Fatalf("Barrer no debe devolver error global: %v", err)
	}
	if res.Nuevos != 0 {
		t.Errorf("nuevos = %d", res.Nuevos)
	}
}
