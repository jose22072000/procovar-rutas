package ingest

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

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/metrics"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Type de barrido.
const (
	// Incremental: solo lo modificado desde el último cursor. Barato, cada 30 min.
	TypeIncremental = "incremental"
	// Nocturno: la carpeta entera, ignorando el cursor. Es el que garantiza que
	// no falte nada aunque un file llegue renombrado, movido o con la date
	// cambiada. Por eso el ritmo del incremental da igual.
	TypeNightly = "nocturno"
	// Backfill: todo el histórico, en el primer arranque.
	TipoBackfill = "backfill"
	// Manual: desde la pantalla de administración.
	TipoManual = "manual"
)

// Accounts es de dónde sale el cliente de Drive de cada carpeta. Hay una cuenta
// de Google por sucursal, así que no vale con un único cliente.
type Accounts interface {
	Para(ctx context.Context, clave string) (drive.Cliente, error)
}

// unaSola adapta un cliente suelto a la interfaz, para las pruebas y para el
// caso en que todas las carpetas viven en la misma cuenta.
type unaSola struct{ cli drive.Cliente }

func (u unaSola) Para(context.Context, string) (drive.Cliente, error) { return u.cli, nil }

// SingleAccount envuelve un cliente único.
func SingleAccount(cli drive.Cliente) Accounts { return unaSola{cli: cli} }

type Service struct {
	pool    *pgxpool.Pool
	q       *store.Queries
	cuentas Accounts
	log     *slog.Logger
	max     int
}

func NewService(pool *pgxpool.Pool, cuentas Accounts, log *slog.Logger, maxFicheros int) *Service {
	if maxFicheros <= 0 {
		maxFicheros = 500
	}
	return &Service{pool: pool, q: store.New(pool), cuentas: cuentas, log: log, max: maxFicheros}
}

// Summary de un barrido.
type Summary struct {
	Seen      int
	New       int
	Failed    int
	Points    int64
	Ausencias int64
}

// Scan recorre todas las fuentes activas.
//
// Un fallo en una carpeta no detiene las demás: se anota en la source y se
// sigue. Que el Drive de una sucursal esté caído no puede dejar sin datos a las
// otras siete.
func (s *Service) Scan(ctx context.Context, tipo string) (Summary, error) {
	fuentes, err := s.q.ActiveSources(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("leyendo fuentes: %w", err)
	}

	total := Summary{}
	for _, f := range fuentes {
		r, err := s.ScanSource(ctx, f, tipo)
		total.Seen += r.Seen
		total.New += r.New
		total.Failed += r.Failed
		total.Points += r.Points
		if err != nil {
			s.log.Error("source fallida", "source", f.Name, "error", err)
			_ = s.q.MarkSourceError(ctx, store.MarkSourceErrorParams{
				ID: f.ID, LastError: puntero(err.Error()),
			})
		}
	}

	return total, nil
}

// ScanSource procesa una carpeta.
func (s *Service) ScanSource(ctx context.Context, source store.DriveSource, tipo string) (Summary, error) {
	res := Summary{}

	registro, err := s.q.OpenImportLog(ctx, store.OpenImportLogParams{
		ID: nuevoID(), SourceID: &source.ID, Type: tipo,
	})
	if err != nil {
		return res, fmt.Errorf("abriendo registro de importación: %w", err)
	}

	// Solo el incremental usa el cursor. El nocturno y el backfill recorren todo
	// a propósito.
	var desde time.Time
	if tipo == TypeIncremental && source.ModifiedCursor != nil {
		desde = *source.ModifiedCursor
	}

	cli, err := s.cuentas.Para(ctx, source.Credential)
	if err != nil {
		return res, fmt.Errorf("credencial %q: %w", source.Credential, err)
	}
	// Sin cliente de Drive no se puede barrer, pero eso NO es motivo para caerse:
	// mientras la entrada sea el empuje de n8n, el sistema funciona igual. Antes
	// esto reventaba con un puntero nulo en mitad del barrido.
	if cli == nil {
		return res, fmt.Errorf("no hay acceso a Drive para la carpeta %q; solo entrará lo que empuje n8n", source.Name)
	}

	ficheros, errListar := cli.Listar(ctx, source.FolderID, desde, s.max)
	res.Seen = len(ficheros)

	alias, err := s.aliasMap(ctx)
	if err != nil {
		return res, err
	}
	zona := s.sourceZone(ctx, source)

	masReciente := desde
	diasTocados := map[claveDia]bool{}

	for _, f := range ficheros {
		nuevo, points, err := s.processFile(ctx, cli, source, f, alias, zona, diasTocados)
		switch {
		case err != nil:
			res.Failed++
			s.log.Warn("file fallido", "file", f.Name, "error", err)
		case nuevo:
			res.New++
			res.Points += points
		}
		if f.Modified.After(masReciente) {
			masReciente = f.Modified
		}
	}

	// Recalcular cada día tocado UNA vez, aunque lo hayan tocado varios ficheros.
	for d := range diasTocados {
		if err := s.RecomputeDay(ctx, d.trabajador, d.date); err != nil {
			s.log.Error("recálculo fallido", "trabajador", d.trabajador, "date", d.date, "error", err)
		}
	}

	detalle := ""
	if errListar != nil {
		detalle = errListar.Error()
	}
	_ = s.q.CloseImportLog(ctx, store.CloseImportLogParams{
		ID:             registro.ID,
		FilesSeen:      int32(res.Seen),
		FilesNew:       int32(res.New),
		FilesFailed:    int32(res.Failed),
		InsertedPoints: int32(res.Points),
		Ok:             errListar == nil,
		Detail:         opcional(detalle),
	})

	if errListar != nil {
		return res, errListar
	}

	// El cursor solo avanza si el listado terminó bien. Avanzarlo tras un fallo
	// parcial dejaría un agujero permanente en el histórico que solo el repaso
	// nocturno taparía.
	if err := s.q.UpdateSourceCursor(ctx, store.UpdateSourceCursorParams{
		ID: source.ID, ModifiedCursor: &masReciente,
	}); err != nil {
		return res, fmt.Errorf("guardando cursor: %w", err)
	}

	return res, nil
}

type claveDia struct {
	trabajador string
	date       time.Time
}

// processFile baja un .gpx, lo juzga y lo guarda. Devuelve si era nuevo.
func (s *Service) processFile(
	ctx context.Context,
	cli drive.Cliente,
	source store.DriveSource,
	f drive.File,
	alias map[string]string,
	zona *time.Location,
	diasTocados map[claveDia]bool,
) (bool, int64, error) {
	// ¿Ya lo tenemos? Se comprueba ANTES de descargar: en el repaso nocturno,
	// que relista carpetas enteras, esto ahorra bajar miles de ficheros que ya
	// están en la base.
	previo, err := s.q.FileByDriveID(ctx, f.ID)
	yaEstaba := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, 0, fmt.Errorf("consultando file: %w", err)
	}
	// Si el tamaño y la date de modificación no han cambiado, es el mismo
	// file: no hay nada que hacer.
	if yaEstaba && previo.Status == store.FileProcessed &&
		previo.SizeBytes != nil && int64(*previo.SizeBytes) == f.Size &&
		!f.Modified.After(previo.ImportedAt) {
		return false, 0, nil
	}

	datos, err := cli.Descargar(ctx, f.ID)
	if err != nil {
		return false, 0, fmt.Errorf("descargando %s: %w", f.Name, err)
	}

	return s.Save(ctx, source, f, datos, alias, zona, diasTocados)
}

// Save mete en la base un .gpx ya descargado.
//
// Está separado de la descarga porque los ficheros llegan por dos caminos: los
// baja el barrido, o los empuja n8n cuando el vendedor los sube (ver
// POST /api/ingesta/file). La decisión y el guardado son los mismos en ambos
// casos; lo único que cambia es quién trae los bytes.
func (s *Service) Save(
	ctx context.Context,
	source store.DriveSource,
	f drive.File,
	datos []byte,
	alias map[string]string,
	zona *time.Location,
	diasTocados map[claveDia]bool,
) (bool, int64, error) {
	previo, err := s.q.FileByDriveID(ctx, f.ID)
	yaEstaba := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, 0, fmt.Errorf("consultando file: %w", err)
	}

	suma := sha256.Sum256(datos)
	hash := hex.EncodeToString(suma[:])

	// El mismo contenido en otra carpeta (o copiado por el propio vendedor) no
	// se ingiere dos veces.
	if otro, err := s.q.FileBySha(ctx, hash); err == nil && otro.DriveFileID != f.ID {
		s.log.Debug("contenido duplicado", "file", f.Name, "ya_estaba_como", otro.Name)
		return false, 0, nil
	}

	v := Examine(f, datos, Env{
		SourceType:     gpx.SourceType(source.Type),
		SourceSellerID: valor(source.SellerID),
		SourceName:     source.Name,
		Alias:          alias,
		Zone:           zona,
	})

	sucursalID := source.BranchID
	if v.SellerID != "" {
		if t, err := s.q.SellerByID(ctx, v.SellerID); err == nil {
			sucursalID = &t.BranchID
		}
	}

	var date *time.Time
	if v.Date != "" {
		if d, err := time.Parse("2006-01-02", v.Date); err == nil {
			date = &d
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	fila, err := qtx.SaveFile(ctx, store.SaveFileParams{
		ID:             idFichero(yaEstaba, previo.ID),
		SourceID:       source.ID,
		DriveFileID:    f.ID,
		Sha256:         hash,
		Name:           f.Name,
		FolderPath:     opcional(rutaTexto(f.FolderPath)),
		SizeBytes:      puntero(int32(f.Size)),
		DriveCreatedAt: horaOpcional(f.Created),
		Status:         store.FileStatus(v.Status),
		Error:          opcional(v.Error),
		SellerID:       opcional(v.SellerID),
		BranchID:       sucursalID,
		Date:           date,
		DateSource:     store.DateSource(v.DateSource),
		TotalPoints:    int32(cuentaPuntos(v)),
		ValidPoints:    int32(cuentaPuntos(v) - cuentaSinHora(v)),
		FirstFix:       primerFix(v),
		LastFix:        ultimoFix(v),
		AliasHint:      opcional(v.AliasHint),
	})
	if err != nil {
		return false, 0, fmt.Errorf("guardando file: %w", err)
	}

	var insertados int64
	if v.Parsed != nil && len(v.Parsed.Points) > 0 {
		// Reemplazo, no acumulación: si el file se re-subió corregido, sus
		// points viejos se van. Así reprocesar es siempre seguro.
		if err := qtx.DeleteFilePoints(ctx, fila.ID); err != nil {
			return false, 0, err
		}
		insertados, err = store.InsertPoints(ctx, tx, pointRows(fila, v))
		if err != nil {
			return false, 0, fmt.Errorf("volcando points: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}

	if v.SellerID != "" && date != nil {
		diasTocados[claveDia{trabajador: v.SellerID, date: *date}] = true
	}

	return true, insertados, nil
}

// RecomputeDay rehace el veredicto de un día desde la base.
//
// Se recalcula desde los PUNTOS GUARDADOS y no desde el file recién llegado
// porque un mismo día puede tener varios ficheros —una sesión de mañana y otra
// de tarde—, y juzgarlos por separado daría dos veredictos contradictorios para
// el mismo día.
func (s *Service) RecomputeDay(ctx context.Context, trabajadorID string, date time.Time) error {
	trab, err := s.q.SellerByID(ctx, trabajadorID)
	if err != nil {
		return fmt.Errorf("trabajador %s: %w", trabajadorID, err)
	}

	cfg, zona := s.branchSettings(ctx, trab.BranchID)

	filas, err := s.q.SellerPointsOnDate(ctx, store.SellerPointsOnDateParams{
		SellerID: &trabajadorID,
		Zone:     zona.String(),
		Date:     date,
	})
	if err != nil {
		return fmt.Errorf("leyendo points: %w", err)
	}

	points := make([]metrics.InputPoint, 0, len(filas))
	for i, p := range filas {
		points = append(points, metrics.InputPoint{
			Lat: p.Lat, Lon: p.Lon, Ts: p.Ts, Accuracy: p.Accuracy, Seq: i,
		})
	}

	res := metrics.ComputeDay(points, cfg)

	dia, err := s.q.SaveDay(ctx, store.SaveDayParams{
		ID:          idDia(trabajadorID, date),
		SellerID:    trabajadorID,
		BranchID:    trab.BranchID,
		Date:        date,
		Status:      store.DayStatus(res.Status),
		FirstFix:    res.FirstFix,
		LastFix:     res.LastFix,
		NetKm:       res.NetKm,
		MinMovement: int32(res.MinMovement),
		MinStopped:  int32(res.MinStopped),
		Coverage:    res.Coverage,
		Gaps:        int32(res.Gaps),
		Points:      int32(res.ValidPoints),
		SpreadM:     &res.SpreadM,
		CentroidLat: latDe(res.Centroid),
		CentroidLon: lonDe(res.Centroid),
		Flags:       res.Flags,
	})
	if err != nil {
		return fmt.Errorf("guardando día: %w", err)
	}

	if err := s.q.DeleteDayStops(ctx, dia.ID); err != nil {
		return err
	}
	for _, p := range res.Stops {
		radio := p.RadiusM
		if err := s.q.CreateStop(ctx, store.CreateStopParams{
			ID:          nuevoID(),
			TrackDayID:  dia.ID,
			SellerID:    trabajadorID,
			BranchID:    trab.BranchID,
			Start:       p.Start,
			End:         p.End,
			DurationMin: int32(p.DurationMin),
			Lat:         p.Lat,
			Lon:         p.Lon,
			Radius:      &radio,
			Seq:         int32(p.Seq),
		}); err != nil {
			return err
		}
	}

	return nil
}

// MarkAbsences crea la fila SIN_FICHERO de cada vendedor que no subió nada
// ese día laborable. Es lo que convierte una ausencia en un dato consultable.
func (s *Service) MarkAbsences(ctx context.Context, date time.Time) (int64, error) {
	return s.q.MarkAbsences(ctx, store.MarkAbsencesParams{
		Date:     date,
		BranchID: "",
	})
}

// --- auxiliares -------------------------------------------------------------

func (s *Service) aliasMap(ctx context.Context) (map[string]string, error) {
	filas, err := s.q.AllAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("leyendo alias: %w", err)
	}
	m := make(map[string]string, len(filas))
	for _, a := range filas {
		m[a.Alias] = a.SellerID
	}
	return m, nil
}

func (s *Service) sourceZone(ctx context.Context, f store.DriveSource) *time.Location {
	if f.BranchID == nil {
		return zonaPorDefecto()
	}
	suc, err := s.q.BranchByID(ctx, *f.BranchID)
	if err != nil {
		return zonaPorDefecto()
	}
	if z, err := time.LoadLocation(suc.Timezone); err == nil {
		return z
	}
	return zonaPorDefecto()
}

// branchSettings trae los umbrales; si la sucursal no tiene fila propia, los
// de fábrica.
func (s *Service) branchSettings(ctx context.Context, sucursalID string) (metrics.Config, *time.Location) {
	cfg := metrics.DefaultConfig()

	if suc, err := s.q.BranchByID(ctx, sucursalID); err == nil {
		if z, err := time.LoadLocation(suc.Timezone); err == nil {
			cfg.Zone = z
		}
	}

	c, err := s.q.BranchConfig(ctx, sucursalID)
	if err != nil {
		return cfg, cfg.Zone
	}

	cfg.WorkdayStart = c.WorkdayStart
	cfg.WorkdayEnd = c.WorkdayEnd
	cfg.StopRadiusM = float64(c.StopRadiusM)
	cfg.StopMinutes = int(c.StopMinutes)
	cfg.MinStepM = float64(c.MinStepM)
	cfg.NoMoveRadiusM = float64(c.NoMoveRadiusM)
	cfg.NoMoveSpanMin = int(c.NoMoveSpanMin)
	cfg.LowNetKm = c.LowNetKm
	cfg.MaxSpeedKmh = float64(c.MaxSpeedKmh)
	cfg.MaxAccuracyM = float64(c.MaxAccuracyM)
	cfg.GapMinutes = int(c.GapMinutes)
	cfg.CoverageGapMin = float64(c.CoverageGapMin)
	cfg.MinCoverage = c.MinCoverage
	cfg.EntryToleranceMin = int(c.EntryToleranceMin)
	cfg.ExitToleranceMin = int(c.ExitToleranceMin)

	return cfg, cfg.Zone
}

func pointRows(fila store.GpxFile, v Verdict) []store.NewPoint {
	cfg := metrics.DefaultConfig()
	entrada := make([]metrics.InputPoint, 0, len(v.Parsed.Points))
	for _, p := range v.Parsed.Points {
		entrada = append(entrada, metrics.InputPoint{
			Lat: p.Lat, Lon: p.Lon, Ts: p.Ts, Accuracy: p.Accuracy, Seq: p.Seq,
		})
	}
	evaluados := metrics.ScorePoints(entrada, cfg)

	out := make([]store.NewPoint, 0, len(evaluados))
	for _, p := range evaluados {
		var vel *float64
		if p.Speed > 0 {
			v := p.Speed
			vel = &v
		}
		out = append(out, store.NewPoint{
			GpxFileID: fila.ID,
			SellerID:  fila.SellerID,
			BranchID:  fila.BranchID,
			Ts:        p.Ts,
			Lat:       p.Lat,
			Lon:       p.Lon,
			Speed:     vel,
			Accuracy:  p.Accuracy,
			Seq:       int32(p.Seq),
			Quality:   store.PointQuality(p.Quality),
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

func cuentaPuntos(v Verdict) int {
	if v.Parsed == nil {
		return 0
	}
	return len(v.Parsed.Points)
}

func cuentaSinHora(v Verdict) int {
	if v.Parsed == nil {
		return 0
	}
	return v.Parsed.NoTime
}

func primerFix(v Verdict) *time.Time {
	if v.Parsed == nil {
		return nil
	}
	return v.Parsed.FirstFix
}

func ultimoFix(v Verdict) *time.Time {
	if v.Parsed == nil {
		return nil
	}
	return v.Parsed.LastFix
}

func latDe(c *metrics.Coord) *float64 {
	if c == nil {
		return nil
	}
	return &c.Lat
}

func lonDe(c *metrics.Coord) *float64 {
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

func idDia(trabajadorID string, date time.Time) string {
	suma := sha256.Sum256([]byte(trabajadorID + ":" + date.Format("2006-01-02")))
	return hex.EncodeToString(suma[:16])
}

func nuevoID() string {
	b := make([]byte, 16)
	if _, err := randRead(b); err != nil {
		// Sin aleatoriedad no se puede generar un identificador único; mejor un
		// pánico aquí que dos filas compartiendo id.
		panic(fmt.Sprintf("sin source de aleatoriedad: %v", err))
	}
	return hex.EncodeToString(b)
}

// randRead se aísla en una variable para poder falsearlo en las pruebas.
var randRead = cryptorand.Read
