package ingesta

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/metricas"
)

// Tipo de barrido.
const (
	// Incremental: solo lo modificado desde el último cursor. Barato, cada 30 min.
	TipoIncremental = "incremental"
	// Nocturno: la carpeta entera, ignorando el cursor. Es el que garantiza que
	// no falte nada aunque un fichero llegue renombrado, movido o con la fecha
	// cambiada. Por eso el ritmo del incremental da igual.
	TipoNocturno = "nocturno"
	// Backfill: todo el histórico, en el primer arranque.
	TipoBackfill = "backfill"
	// Manual: desde la pantalla de administración.
	TipoManual = "manual"
)

// Cuentas es de dónde sale el cliente de Drive de cada carpeta. Hay una cuenta
// de Google por sucursal, así que no vale con un único cliente.
type Cuentas interface {
	Para(ctx context.Context, clave string) (drive.Cliente, error)
}

// unaSola adapta un cliente suelto a la interfaz, para las pruebas y para el
// caso en que todas las carpetas viven en la misma cuenta.
type unaSola struct{ cli drive.Cliente }

func (u unaSola) Para(context.Context, string) (drive.Cliente, error) { return u.cli, nil }

// UnaCuenta envuelve un cliente único.
func UnaCuenta(cli drive.Cliente) Cuentas { return unaSola{cli: cli} }

type Servicio struct {
	pool    *pgxpool.Pool
	q       *almacen.Queries
	cuentas Cuentas
	log     *slog.Logger
	max     int
}

func NuevoServicio(pool *pgxpool.Pool, cuentas Cuentas, log *slog.Logger, maxFicheros int) *Servicio {
	if maxFicheros <= 0 {
		maxFicheros = 500
	}
	return &Servicio{pool: pool, q: almacen.New(pool), cuentas: cuentas, log: log, max: maxFicheros}
}

// Resumen de un barrido.
type Resumen struct {
	Vistos    int
	Nuevos    int
	Errores   int
	Puntos    int64
	Ausencias int64
}

// Barrer recorre todas las fuentes activas.
//
// Un fallo en una carpeta no detiene las demás: se anota en la fuente y se
// sigue. Que el Drive de una sucursal esté caído no puede dejar sin datos a las
// otras siete.
func (s *Servicio) Barrer(ctx context.Context, tipo string) (Resumen, error) {
	fuentes, err := s.q.FuentesActivas(ctx)
	if err != nil {
		return Resumen{}, fmt.Errorf("leyendo fuentes: %w", err)
	}

	total := Resumen{}
	for _, f := range fuentes {
		r, err := s.BarrerFuente(ctx, f, tipo)
		total.Vistos += r.Vistos
		total.Nuevos += r.Nuevos
		total.Errores += r.Errores
		total.Puntos += r.Puntos
		if err != nil {
			s.log.Error("fuente fallida", "fuente", f.Nombre, "error", err)
			_ = s.q.MarcarErrorFuente(ctx, almacen.MarcarErrorFuenteParams{
				ID: f.ID, UltimoError: puntero(err.Error()),
			})
		}
	}

	return total, nil
}

// BarrerFuente procesa una carpeta.
func (s *Servicio) BarrerFuente(ctx context.Context, fuente almacen.DriveSource, tipo string) (Resumen, error) {
	res := Resumen{}

	registro, err := s.q.AbrirImportLog(ctx, almacen.AbrirImportLogParams{
		ID: nuevoID(), SourceID: &fuente.ID, Tipo: tipo,
	})
	if err != nil {
		return res, fmt.Errorf("abriendo registro de importación: %w", err)
	}

	// Solo el incremental usa el cursor. El nocturno y el backfill recorren todo
	// a propósito.
	var desde time.Time
	if tipo == TipoIncremental && fuente.CursorModificado != nil {
		desde = *fuente.CursorModificado
	}

	cli, err := s.cuentas.Para(ctx, fuente.Credencial)
	if err != nil {
		return res, fmt.Errorf("credencial %q: %w", fuente.Credencial, err)
	}

	ficheros, errListar := cli.Listar(ctx, fuente.FolderID, desde, s.max)
	res.Vistos = len(ficheros)

	alias, err := s.mapaAlias(ctx)
	if err != nil {
		return res, err
	}
	zona := s.zonaDeFuente(ctx, fuente)

	masReciente := desde
	diasTocados := map[claveDia]bool{}

	for _, f := range ficheros {
		nuevo, puntos, err := s.procesarFichero(ctx, cli, fuente, f, alias, zona, diasTocados)
		switch {
		case err != nil:
			res.Errores++
			s.log.Warn("fichero fallido", "fichero", f.Nombre, "error", err)
		case nuevo:
			res.Nuevos++
			res.Puntos += puntos
		}
		if f.Modificado.After(masReciente) {
			masReciente = f.Modificado
		}
	}

	// Recalcular cada día tocado UNA vez, aunque lo hayan tocado varios ficheros.
	for d := range diasTocados {
		if err := s.RecalcularDia(ctx, d.trabajador, d.fecha); err != nil {
			s.log.Error("recálculo fallido", "trabajador", d.trabajador, "fecha", d.fecha, "error", err)
		}
	}

	detalle := ""
	if errListar != nil {
		detalle = errListar.Error()
	}
	_ = s.q.CerrarImportLog(ctx, almacen.CerrarImportLogParams{
		ID:               registro.ID,
		FicherosVistos:   int32(res.Vistos),
		FicherosNuevos:   int32(res.Nuevos),
		FicherosError:    int32(res.Errores),
		PuntosInsertados: int32(res.Puntos),
		Ok:               errListar == nil,
		Detalle:          opcional(detalle),
	})

	if errListar != nil {
		return res, errListar
	}

	// El cursor solo avanza si el listado terminó bien. Avanzarlo tras un fallo
	// parcial dejaría un agujero permanente en el histórico que solo el repaso
	// nocturno taparía.
	if err := s.q.ActualizarCursorFuente(ctx, almacen.ActualizarCursorFuenteParams{
		ID: fuente.ID, CursorModificado: &masReciente,
	}); err != nil {
		return res, fmt.Errorf("guardando cursor: %w", err)
	}

	return res, nil
}

type claveDia struct {
	trabajador string
	fecha      time.Time
}

// procesarFichero baja un .gpx, lo juzga y lo guarda. Devuelve si era nuevo.
func (s *Servicio) procesarFichero(
	ctx context.Context,
	cli drive.Cliente,
	fuente almacen.DriveSource,
	f drive.Fichero,
	alias map[string]string,
	zona *time.Location,
	diasTocados map[claveDia]bool,
) (bool, int64, error) {
	// ¿Ya lo tenemos? Se comprueba ANTES de descargar: en el repaso nocturno,
	// que relista carpetas enteras, esto ahorra bajar miles de ficheros que ya
	// están en la base.
	previo, err := s.q.FicheroPorDriveID(ctx, f.ID)
	yaEstaba := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, 0, fmt.Errorf("consultando fichero: %w", err)
	}
	// Si el tamaño y la fecha de modificación no han cambiado, es el mismo
	// fichero: no hay nada que hacer.
	if yaEstaba && previo.Estado == almacen.EstadoFicheroPROCESADO &&
		previo.TamanoBytes != nil && int64(*previo.TamanoBytes) == f.Tamano &&
		!f.Modificado.After(previo.ImportadoAt) {
		return false, 0, nil
	}

	datos, err := cli.Descargar(ctx, f.ID)
	if err != nil {
		return false, 0, fmt.Errorf("descargando %s: %w", f.Nombre, err)
	}

	return s.Guardar(ctx, fuente, f, datos, alias, zona, diasTocados)
}

// Guardar mete en la base un .gpx ya descargado.
//
// Está separado de la descarga porque los ficheros llegan por dos caminos: los
// baja el barrido, o los empuja n8n cuando el vendedor los sube (ver
// POST /api/ingesta/fichero). La decisión y el guardado son los mismos en ambos
// casos; lo único que cambia es quién trae los bytes.
func (s *Servicio) Guardar(
	ctx context.Context,
	fuente almacen.DriveSource,
	f drive.Fichero,
	datos []byte,
	alias map[string]string,
	zona *time.Location,
	diasTocados map[claveDia]bool,
) (bool, int64, error) {
	previo, err := s.q.FicheroPorDriveID(ctx, f.ID)
	yaEstaba := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, 0, fmt.Errorf("consultando fichero: %w", err)
	}

	suma := sha256.Sum256(datos)
	hash := hex.EncodeToString(suma[:])

	// El mismo contenido en otra carpeta (o copiado por el propio vendedor) no
	// se ingiere dos veces.
	if otro, err := s.q.FicheroPorSha(ctx, hash); err == nil && otro.DriveFileID != f.ID {
		s.log.Debug("contenido duplicado", "fichero", f.Nombre, "ya_estaba_como", otro.Nombre)
		return false, 0, nil
	}

	v := Examinar(f, datos, Entorno{
		TipoFuente:         gpx.TipoFuente(fuente.Tipo),
		TrabajadorIDFuente: valor(fuente.TrabajadorID),
		Alias:              alias,
		Zona:               zona,
	})

	sucursalID := fuente.SucursalID
	if v.TrabajadorID != "" {
		if t, err := s.q.TrabajadorPorID(ctx, v.TrabajadorID); err == nil {
			sucursalID = &t.SucursalID
		}
	}

	var fecha *time.Time
	if v.Fecha != "" {
		if d, err := time.Parse("2006-01-02", v.Fecha); err == nil {
			fecha = &d
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	fila, err := qtx.GuardarFichero(ctx, almacen.GuardarFicheroParams{
		ID:             idFichero(yaEstaba, previo.ID),
		SourceID:       fuente.ID,
		DriveFileID:    f.ID,
		Sha256:         hash,
		Nombre:         f.Nombre,
		RutaCarpeta:    opcional(rutaTexto(f.RutaCarpeta)),
		TamanoBytes:    puntero(int32(f.Tamano)),
		DriveCreatedAt: horaOpcional(f.Creado),
		Estado:         almacen.EstadoFichero(v.Estado),
		Error:          opcional(v.Error),
		TrabajadorID:   opcional(v.TrabajadorID),
		SucursalID:     sucursalID,
		Fecha:          fecha,
		OrigenFecha:    almacen.OrigenFecha(v.OrigenFecha),
		PuntosTotal:    int32(cuentaPuntos(v)),
		PuntosValidos:  int32(cuentaPuntos(v) - cuentaSinHora(v)),
		PrimerFix:      primerFix(v),
		UltimoFix:      ultimoFix(v),
		PistaAlias:     opcional(v.PistaAlias),
	})
	if err != nil {
		return false, 0, fmt.Errorf("guardando fichero: %w", err)
	}

	var insertados int64
	if v.Parseado != nil && len(v.Parseado.Puntos) > 0 {
		// Reemplazo, no acumulación: si el fichero se re-subió corregido, sus
		// puntos viejos se van. Así reprocesar es siempre seguro.
		if err := qtx.BorrarPuntosDeFichero(ctx, fila.ID); err != nil {
			return false, 0, err
		}
		insertados, err = almacen.InsertarPuntos(ctx, tx, filasDePuntos(fila, v))
		if err != nil {
			return false, 0, fmt.Errorf("volcando puntos: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}

	if v.TrabajadorID != "" && fecha != nil {
		diasTocados[claveDia{trabajador: v.TrabajadorID, fecha: *fecha}] = true
	}

	return true, insertados, nil
}

// RecalcularDia rehace el veredicto de un día desde la base.
//
// Se recalcula desde los PUNTOS GUARDADOS y no desde el fichero recién llegado
// porque un mismo día puede tener varios ficheros —una sesión de mañana y otra
// de tarde—, y juzgarlos por separado daría dos veredictos contradictorios para
// el mismo día.
func (s *Servicio) RecalcularDia(ctx context.Context, trabajadorID string, fecha time.Time) error {
	trab, err := s.q.TrabajadorPorID(ctx, trabajadorID)
	if err != nil {
		return fmt.Errorf("trabajador %s: %w", trabajadorID, err)
	}

	cfg, zona := s.configDeSucursal(ctx, trab.SucursalID)

	filas, err := s.q.PuntosDeTrabajadorEnFecha(ctx, almacen.PuntosDeTrabajadorEnFechaParams{
		TrabajadorID: &trabajadorID,
		Zona:         zona.String(),
		Fecha:        fecha,
	})
	if err != nil {
		return fmt.Errorf("leyendo puntos: %w", err)
	}

	puntos := make([]metricas.PuntoEntrada, 0, len(filas))
	for i, p := range filas {
		puntos = append(puntos, metricas.PuntoEntrada{
			Lat: p.Lat, Lon: p.Lon, Ts: p.Ts, Accuracy: p.Accuracy, Seq: i,
		})
	}

	res := metricas.CalcularDia(puntos, cfg)

	dia, err := s.q.GuardarDia(ctx, almacen.GuardarDiaParams{
		ID:              idDia(trabajadorID, fecha),
		TrabajadorID:    trabajadorID,
		SucursalID:      trab.SucursalID,
		Fecha:           fecha,
		Estado:          almacen.EstadoDia(res.Estado),
		PrimerFix:       res.PrimerFix,
		UltimoFix:       res.UltimoFix,
		KmNetos:         res.KmNetos,
		MinMovimiento:   int32(res.MinMovimiento),
		MinParado:       int32(res.MinParado),
		Cobertura:       res.Cobertura,
		Huecos:          int32(res.Huecos),
		Puntos:          int32(res.PuntosValidos),
		RadioDispersion: &res.RadioDispersion,
		CentroideLat:    latDe(res.Centroide),
		CentroideLon:    lonDe(res.Centroide),
		Banderas:        res.Banderas,
	})
	if err != nil {
		return fmt.Errorf("guardando día: %w", err)
	}

	if err := s.q.BorrarParadasDeDia(ctx, dia.ID); err != nil {
		return err
	}
	for _, p := range res.Paradas {
		radio := p.RadioM
		if err := s.q.CrearParada(ctx, almacen.CrearParadaParams{
			ID:           nuevoID(),
			TrackDayID:   dia.ID,
			TrabajadorID: trabajadorID,
			SucursalID:   trab.SucursalID,
			Inicio:       p.Inicio,
			Fin:          p.Fin,
			DuracionMin:  int32(p.DuracionMin),
			Lat:          p.Lat,
			Lon:          p.Lon,
			Radio:        &radio,
			Seq:          int32(p.Seq),
		}); err != nil {
			return err
		}
	}

	return nil
}

// MarcarAusencias crea la fila SIN_FICHERO de cada vendedor que no subió nada
// ese día laborable. Es lo que convierte una ausencia en un dato consultable.
func (s *Servicio) MarcarAusencias(ctx context.Context, fecha time.Time) (int64, error) {
	return s.q.MarcarAusencias(ctx, almacen.MarcarAusenciasParams{
		Fecha:      fecha,
		SucursalID: "",
	})
}

// --- auxiliares -------------------------------------------------------------

func (s *Servicio) mapaAlias(ctx context.Context) (map[string]string, error) {
	filas, err := s.q.AliasTodos(ctx)
	if err != nil {
		return nil, fmt.Errorf("leyendo alias: %w", err)
	}
	m := make(map[string]string, len(filas))
	for _, a := range filas {
		m[a.Alias] = a.TrabajadorID
	}
	return m, nil
}

func (s *Servicio) zonaDeFuente(ctx context.Context, f almacen.DriveSource) *time.Location {
	if f.SucursalID == nil {
		return zonaPorDefecto()
	}
	suc, err := s.q.SucursalPorID(ctx, *f.SucursalID)
	if err != nil {
		return zonaPorDefecto()
	}
	if z, err := time.LoadLocation(suc.Timezone); err == nil {
		return z
	}
	return zonaPorDefecto()
}

// configDeSucursal trae los umbrales; si la sucursal no tiene fila propia, los
// de fábrica.
func (s *Servicio) configDeSucursal(ctx context.Context, sucursalID string) (metricas.Config, *time.Location) {
	cfg := metricas.ConfigPorDefecto()

	if suc, err := s.q.SucursalPorID(ctx, sucursalID); err == nil {
		if z, err := time.LoadLocation(suc.Timezone); err == nil {
			cfg.Zona = z
		}
	}

	c, err := s.q.ConfigDeSucursal(ctx, sucursalID)
	if err != nil {
		return cfg, cfg.Zona
	}

	cfg.JornadaInicio = c.JornadaInicio
	cfg.JornadaFin = c.JornadaFin
	cfg.ParadaRadioM = float64(c.ParadaRadioM)
	cfg.ParadaMinutos = int(c.ParadaMinutos)
	cfg.PasoMinimoM = float64(c.PasoMinimoM)
	cfg.SinMovRadioM = float64(c.SinMovRadioM)
	cfg.SinMovSpanMin = int(c.SinMovSpanMin)
	cfg.EscasoKmNetos = c.EscasoKmNetos
	cfg.MaxVelocidadKmh = float64(c.MaxVelocidadKmh)
	cfg.MaxAccuracyM = float64(c.MaxAccuracyM)
	cfg.HuecoMinutos = int(c.HuecoMinutos)
	cfg.CoberturaGapMin = float64(c.CoberturaGapMin)
	cfg.CoberturaMinima = c.CoberturaMinima
	cfg.ToleranciaEntradaMin = int(c.ToleranciaEntradaMin)
	cfg.ToleranciaSalidaMin = int(c.ToleranciaSalidaMin)

	return cfg, cfg.Zona
}

func filasDePuntos(fila almacen.GpxFile, v Veredicto) []almacen.PuntoNuevo {
	cfg := metricas.ConfigPorDefecto()
	entrada := make([]metricas.PuntoEntrada, 0, len(v.Parseado.Puntos))
	for _, p := range v.Parseado.Puntos {
		entrada = append(entrada, metricas.PuntoEntrada{
			Lat: p.Lat, Lon: p.Lon, Ts: p.Ts, Accuracy: p.Accuracy, Seq: p.Seq,
		})
	}
	evaluados := metricas.EvaluarPuntos(entrada, cfg)

	out := make([]almacen.PuntoNuevo, 0, len(evaluados))
	for _, p := range evaluados {
		var vel *float64
		if p.Speed > 0 {
			v := p.Speed
			vel = &v
		}
		out = append(out, almacen.PuntoNuevo{
			GpxFileID:    fila.ID,
			TrabajadorID: fila.TrabajadorID,
			SucursalID:   fila.SucursalID,
			Ts:           p.Ts,
			Lat:          p.Lat,
			Lon:          p.Lon,
			Speed:        vel,
			Accuracy:     p.Accuracy,
			Seq:          int32(p.Seq),
			Quality:      almacen.CalidadPunto(p.Quality),
		})
	}
	return out
}

func zonaPorDefecto() *time.Location {
	if z, err := time.LoadLocation("America/Havana"); err == nil {
		return z
	}
	return time.FixedZone("CUT", -4*3600)
}

func cuentaPuntos(v Veredicto) int {
	if v.Parseado == nil {
		return 0
	}
	return len(v.Parseado.Puntos)
}

func cuentaSinHora(v Veredicto) int {
	if v.Parseado == nil {
		return 0
	}
	return v.Parseado.SinHora
}

func primerFix(v Veredicto) *time.Time {
	if v.Parseado == nil {
		return nil
	}
	return v.Parseado.PrimerFix
}

func ultimoFix(v Veredicto) *time.Time {
	if v.Parseado == nil {
		return nil
	}
	return v.Parseado.UltimoFix
}

func latDe(c *metricas.Coord) *float64 {
	if c == nil {
		return nil
	}
	return &c.Lat
}

func lonDe(c *metricas.Coord) *float64 {
	if c == nil {
		return nil
	}
	return &c.Lon
}

func rutaTexto(ruta []string) string {
	out := ""
	for i, r := range ruta {
		if i > 0 {
			out += "/"
		}
		out += r
	}
	return out
}

func opcional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func valor(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func puntero[T any](v T) *T { return &v }

func horaOpcional(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func idFichero(yaEstaba bool, previo string) string {
	if yaEstaba {
		return previo
	}
	return nuevoID()
}

func idDia(trabajadorID string, fecha time.Time) string {
	suma := sha256.Sum256([]byte(trabajadorID + ":" + fecha.Format("2006-01-02")))
	return hex.EncodeToString(suma[:16])
}

func nuevoID() string {
	b := make([]byte, 16)
	if _, err := randRead(b); err != nil {
		// Sin aleatoriedad no se puede generar un identificador único; mejor un
		// pánico aquí que dos filas compartiendo id.
		panic(fmt.Sprintf("sin fuente de aleatoriedad: %v", err))
	}
	return hex.EncodeToString(b)
}

// randRead se aísla en una variable para poder falsearlo en las pruebas.
var randRead = cryptorand.Read
