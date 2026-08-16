package ingest

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/metrics"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Scan type.
const (
	// Incremental: only what changed since the last cursor. Cheap, every 30 min.
	TypeIncremental = "incremental"
	// Nightly: the whole folder, ignoring the cursor. This is what guarantees
	// nothing is missing even if a file arrives renamed, moved or with a changed
	// date. That is why the incremental cadence does not matter.
	TypeNightly = "nocturno"
	// Backfill: the entire history, on first start.
	TipoBackfill = "backfill"
	// Manual: from the administration screen.
	TipoManual = "manual"
)

// Accounts is where each folder's Drive client comes from. There is one Google
// account per branch, so a single client is not enough.
type Accounts interface {
	For(ctx context.Context, clave string) (drive.Client, error)
}

// singleAccount adapts a lone client to the interface, for tests and for the
// case where every folder lives in the same account.
type singleAccount struct{ cli drive.Client }

func (u singleAccount) For(context.Context, string) (drive.Client, error) { return u.cli, nil }

// SingleAccount wraps a lone client.
func SingleAccount(cli drive.Client) Accounts { return singleAccount{cli: cli} }

type Service struct {
	pool     *pgxpool.Pool
	q        *store.Queries
	accounts Accounts
	log      *slog.Logger
	max      int
}

func NewService(pool *pgxpool.Pool, accounts Accounts, log *slog.Logger, maxFicheros int) *Service {
	if maxFicheros <= 0 {
		maxFicheros = 500
	}
	// Sin registro de sucesos se descarta, en vez de dejar un puntero nulo esperando
	// a la primera línea que quiera avisar de algo. Pasa en las pruebas, y reventar
	// ahí esconde el fallo que se estaba probando.
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{pool: pool, q: store.New(pool), accounts: accounts, log: log, max: maxFicheros}
}

// Summary of a scan.
type Summary struct {
	Seen      int
	New       int
	Failed    int
	Points    int64
	Ausencias int64
}

// Scan walks every active source.
//
// A failure in one folder does not stop the rest: it is recorded on the source
// and the scan carries on. One branch's Drive being down cannot leave the
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

// ScanSource processes one folder.
func (s *Service) ScanSource(ctx context.Context, source store.DriveSource, tipo string) (Summary, error) {
	res := Summary{}

	registro, err := s.q.OpenImportLog(ctx, store.OpenImportLogParams{
		ID: newID(), SourceID: &source.ID, Type: tipo,
	})
	if err != nil {
		return res, fmt.Errorf("abriendo registro de importación: %w", err)
	}

	// Only the incremental scan uses the cursor. The nightly and backfill scans
	// deliberately walk everything.
	var desde time.Time
	if tipo == TypeIncremental && source.ModifiedCursor != nil {
		desde = *source.ModifiedCursor
	}

	cli, err := s.accounts.For(ctx, source.Credential)
	if err != nil {
		return res, fmt.Errorf("credencial %q: %w", source.Credential, err)
	}
	// Without a Drive client there is nothing to scan, but that is NOT a reason to
	// fall over: as long as files arrive through n8n's push, the system works just
	// the same. This used to blow up with a nil pointer mid-scan.
	if cli == nil {
		return res, fmt.Errorf("no hay acceso a Drive para la carpeta %q; solo entrará lo que empuje n8n", source.Name)
	}

	ficheros, errListar := cli.List(ctx, source.FolderID, desde, s.max)
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

	// The cursor only advances if the listing finished cleanly. Advancing it after
	// a partial failure would leave a permanent hole in the history that only the
	// nightly sweep would ever cover.
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

// sellerFromFolder devuelve el vendedor que representa una carpeta, creándolo la
// primera vez.
//
// Es find-or-create y no un alta: la ingesta corre cada hora sobre las mismas
// carpetas, así que crear a ciegas duplicaría a la misma persona una vez por
// fichero.
func (s *Service) sellerFromFolder(ctx context.Context, source store.DriveSource) (string, error) {
	branchID, err := s.branchOfSource(ctx, source)
	if err != nil {
		return "", err
	}

	if t, err := s.q.SellerByNameInBranch(ctx, store.SellerByNameInBranchParams{
		Name: source.Name, BranchID: branchID,
	}); err == nil {
		return t.ID, nil
	}

	t, err := s.q.CreateSellerByName(ctx, store.CreateSellerByNameParams{
		ID: newID(), Name: source.Name, BranchID: branchID,
	})
	if err != nil {
		// Otra ingesta pudo crearlo entre la consulta y el alta.
		if t2, err2 := s.q.SellerByNameInBranch(ctx, store.SellerByNameInBranchParams{
			Name: source.Name, BranchID: branchID,
		}); err2 == nil {
			return t2.ID, nil
		}
		return "", err
	}
	return t.ID, nil
}

// branchOfSource: la sucursal de una carpeta es la de la CUENTA de Google desde la
// que se lee. Si la carpeta ya trae sucursal puesta a mano, esa manda.
func (s *Service) branchOfSource(ctx context.Context, source store.DriveSource) (string, error) {
	if source.BranchID != nil && *source.BranchID != "" {
		return *source.BranchID, nil
	}
	return s.branchOfAccount(ctx, source.Credential)
}

// branchOfAccount traduce el nombre de la cuenta de Google a una sucursal, creándola
// la primera vez. Hay una cuenta por sucursal —Camagüey, Holguín, Santiago…— así que
// el nombre de la cuenta ES el de la sucursal.
func (s *Service) branchOfAccount(ctx context.Context, cuenta string) (string, error) {
	nombre := nombreDeCuenta(cuenta)
	if nombre == "" {
		nombre = "principal"
	}

	clave := claveDeSucursal(nombre)
	if suc, err := s.q.BranchByKey(ctx, clave); err == nil {
		return suc.ID, nil
	}
	suc, err := s.q.CreateBranchByKey(ctx, store.CreateBranchByKeyParams{
		ID: newID(), Name: nombre, Key: clave,
	})
	if err != nil {
		if suc2, err2 := s.q.BranchByKey(ctx, clave); err2 == nil {
			return suc2.ID, nil
		}
		return "", err
	}
	return suc.ID, nil
}

// nombreDeCuenta deja el nombre de la sucursal a partir de la cuenta de Google:
// "camaguey.procovar@gmail.com" -> "camaguey". Si no parece un correo, se usa tal
// cual, que es lo que pasa cuando la cuenta se dio de alta por su nombre.
func nombreDeCuenta(cuenta string) string {
	c := strings.TrimSpace(cuenta)

	// Si no es un correo, es el nombre de la cuenta tal como lo enseña Drive
	// ("Camagüey Procovar", "Habana Procovar"). Sobra el apellido de la empresa, y
	// lo que queda es el nombre de la sucursal con sus tildes — igual que en
	// Accesos, que es lo que permite emparejarlas.
	if !strings.Contains(c, "@") {
		return limpiarApellido(c)
	}

	// Si es un correo, se saca la parte útil: las cuentas se llaman de las dos
	// formas, "camaguey.procovar@…" y "habanaprocovar@…", y sobra el apellido en
	// los dos casos.
	c = c[:strings.IndexByte(c, '@')]
	if i := strings.IndexByte(c, '.'); i > 0 {
		c = c[:i]
	}
	return limpiarApellido(c)
}

// claveDeSucursal reduce el nombre a lo que de verdad lo identifica: sin tildes, sin
// espacios y en minúsculas. "Las Tunas", "lastunas" y "LAS TUNAS" son la misma.
//
// El NOMBRE se guarda tal cual para leerlo; la clave es la que manda para decidir si
// una sucursal ya existe.
func claveDeSucursal(nombre string) string {
	sustituye := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
		"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n",
	)
	var b strings.Builder
	for _, r := range sustituye.Replace(strings.ToLower(strings.TrimSpace(nombre))) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// limpiarApellido quita el "procovar" del final, venga como venga.
//
// Las cuentas están escritas de todas las formas: "Camagüey Procovar",
// "santiagoprocovar", "camaguey.procovar@…". Sin quitarlo, la MISMA sucursal
// aparecía dos veces con dos nombres distintos, y un gerente de "Santiago" no vería
// lo que quedó en "santiagoprocovar".
func limpiarApellido(s string) string {
	s = strings.TrimSpace(s)
	bajo := strings.ToLower(s)
	for _, cola := range []string{" procovar", "-procovar", ".procovar", "procovar"} {
		if strings.HasSuffix(bajo, cola) {
			s = strings.TrimSpace(s[:len(s)-len(cola)])
			break
		}
	}
	return s
}

// processFile downloads a .gpx, judges it and stores it. Returns whether it was new.
func (s *Service) processFile(
	ctx context.Context,
	cli drive.Client,
	source store.DriveSource,
	f drive.File,
	alias map[string]string,
	zona *time.Location,
	diasTocados map[claveDia]bool,
) (bool, int64, error) {
	// Do we have it already? Checked BEFORE downloading: in the nightly sweep,
	// which relists whole folders, this saves downloading thousands of files that
	// are already in the database.
	previo, err := s.q.FileByDriveID(ctx, f.ID)
	yaEstaba := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, 0, fmt.Errorf("consultando file: %w", err)
	}
	// If the size and the modified date have not changed it is the same file:
	// there is nothing to do.
	if yaEstaba && previo.Status == store.FileProcessed &&
		previo.SizeBytes != nil && int64(*previo.SizeBytes) == f.Size &&
		!f.Modified.After(previo.ImportedAt) {
		return false, 0, nil
	}

	datos, err := cli.Download(ctx, f.ID)
	if err != nil {
		return false, 0, fmt.Errorf("descargando %s: %w", f.Name, err)
	}

	return s.Save(ctx, source, f, datos, alias, zona, diasTocados)
}

// Save stores an already-downloaded .gpx in the database.
//
// It is kept apart from the download because files arrive by two routes: the scan
// fetches them, or n8n pushes them when the seller uploads (see
// POST /api/ingest/file). The decision and the storing are the same on both
// paths; the only difference is who brings the bytes.
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

	// The same content in another folder (or copied by the seller themselves) is
	// not ingested twice.
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

	// Si nadie lo resolvió, el nombre de la CARPETA es el vendedor.
	//
	// Cada carpeta compartida es el perfil de GPS de una persona ("STGGari",
	// "GPS Diana Acosta"), así que el nombre ya identifica a quién se está mirando.
	// Antes esto se mandaba a la bandeja para que un admin lo casara a mano, y eso
	// era pedirle que teclease lo que la carpeta ya dice: son 53 carpetas, y el
	// gerente que abre el panel mira los GPS de su sucursal sabiendo perfectamente
	// quién es quién.
	//
	// La sucursal sale de la CUENTA de Google de la que vino, que es como está
	// montado: una cuenta por sucursal. Mientras solo haya una cuenta dada de alta,
	// todo cae en su sucursal; al añadir las demás, cada carpeta va a la suya.
	if v.SellerID == "" && source.Name != "" {
		if id, err := s.sellerFromFolder(ctx, source); err != nil {
			s.log.Warn("no se pudo crear el vendedor desde la carpeta",
				"carpeta", source.Name, "error", err)
		} else {
			v.SellerID = id
			v.AliasHint = ""
			// Solo se levanta el "sin asignar", que era lo único que impedía darlo
			// por bueno. Un fichero ilegible o sin fecha CONSERVA su estado: saber de
			// quién es no lo arregla, y marcarlo como procesado escondería el fallo
			// justo donde tiene que verse.
			if v.Status == StatusUnassigned {
				v.Status = StatusProcessed
			}
		}
	}

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
		// Replace, do not accumulate: if the file was re-uploaded after a fix, its
		// old points go. That makes reprocessing always safe.
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

// RecomputeDay rebuilds a day's verdict from the database.
//
// It recomputes from the STORED POINTS rather than from the file that just
// arrived, because one day can have several files — a morning session and an
// afternoon one — and judging them separately would produce two contradictory
// verdicts for the same day.
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
			ID:          newID(),
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

// MarkAbsences creates the SIN_FICHERO row for every seller who uploaded nothing
// on that working day. It is what turns an absence into queryable data.
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

// branchSettings fetches the thresholds; if the branch has no row of its own,
// the factory values.
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
	return newID()
}

func idDia(trabajadorID string, date time.Time) string {
	suma := sha256.Sum256([]byte(trabajadorID + ":" + date.Format("2006-01-02")))
	return hex.EncodeToString(suma[:16])
}

func newID() string {
	b := make([]byte, 16)
	if _, err := randRead(b); err != nil {
		// Without randomness a unique identifier cannot be generated; better a panic
		// here than two rows sharing an id.
		panic(fmt.Sprintf("sin source de aleatoriedad: %v", err))
	}
	return hex.EncodeToString(b)
}

// randRead is kept in a variable so tests can stub it.
var randRead = cryptorand.Read
