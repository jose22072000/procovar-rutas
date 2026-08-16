package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/procovar/procovar-rutas/api/internal/drive"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Ficheros empujados desde fuera.
//
// The built-in scan pulls .gpx files from Drive, but Procovar already runs an n8n
// that watches the folders and works — it is how orders come in — so ingest also
// accepts the opposite direction: n8n sends the file and all that happens here is
// procese.
//
// Both routes end in the same place and are idempotent on `driveFileId` and on
// `sha256`, so they can coexist without duplicating anything: if n8n already put
// today's file in, tonight's scan will see it and skip it.

// Pushed is what n8n sends.
type Pushed struct {
	// Account is the Google account that owns the shared folder: the branch's. It
	// is not the one n8n signs in with — everything comes through the parent
	// account — but the folder's owner, which is what Drive records in `owners`.
	Account string
	// SourceID or FolderID identify the folder already registered.
	SourceID string
	FolderID string
	// DriveFileID is the deduplication key: the same file sent twice does not go
	// in twice.
	DriveFileID string
	Name        string
	// FolderPath is the sub-folders inside the source. It is where the seller comes
	// from when the file name only carries the date.
	FolderPath []string
	Created    time.Time
	Content    []byte
}

// Receive processes a pushed file.
//
// An empty file is recorded rather than refused: a 0-byte .gpx exists in Drive, and
// what has to be visible is that it arrived empty, not nothing at all.
func (s *Service) Receive(ctx context.Context, e Pushed) (bool, int64, error) {
	if e.DriveFileID == "" || e.Name == "" {
		return false, 0, fmt.Errorf("faltan el identificador o el nombre")
	}

	// Primero, de qué sucursal es la carpeta. Va ANTES del corte por fichero
	// repetido a propósito: casi todos los ficheros que llegan en la primera pasada
	// ya están dentro, y si se cortara antes, la carpeta no llegaría a enterarse
	// nunca de a qué sucursal pertenece. Se busca la carpeta SIN crearla: si el
	// empuje viene de una subcarpeta de archivo, no casa con ninguna y no pasa nada.
	if e.Account != "" && e.FolderID != "" {
		if fuente, ok := s.sourcePorCarpeta(ctx, e.FolderID); ok && !s.mismaCuenta(fuente, e.Account) {
			if err := s.reasignarSucursal(ctx, &fuente, e.Account); err != nil {
				s.log.Warn("no se pudo asignar la sucursal de la carpeta",
					"carpeta", fuente.Name, "cuenta", e.Account, "error", err)
			}
		}
	}

	// Lo ya visto se corta AQUÍ, antes de mirar de qué carpeta viene.
	//
	// Los ficheros se archivan en Drive moviéndolos a una subcarpeta ("Procesados",
	// "Errores"), y una búsqueda posterior los encuentra allí: su carpeta padre ya
	// no es el perfil del vendedor sino la de archivo. Si eso llegara a la
	// resolución de carpeta, se daría de alta una fuente —y con ella un "vendedor"—
	// llamada Procesados. Salir antes por el identificador de Drive lo evita, y de
	// paso ahorra el trabajo de volver a parsear lo que ya está.
	if _, err := s.q.FileByDriveID(ctx, e.DriveFileID); err == nil {
		return false, 0, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, 0, fmt.Errorf("consultando el fichero: %w", err)
	}

	source, err := s.findSource(ctx, e)
	if err != nil {
		return false, 0, err
	}

	alias, err := s.aliasMap(ctx)
	if err != nil {
		return false, 0, err
	}
	zona := s.sourceZone(ctx, source)

	f := drive.File{
		ID:         e.DriveFileID,
		Name:       e.Name,
		FolderPath: e.FolderPath,
		Size:       int64(len(e.Content)),
		Created:    e.Created,
		Modified:   e.Created,
	}

	diasTocados := map[claveDia]bool{}
	nuevo, points, err := s.Save(ctx, source, f, e.Content, alias, zona, diasTocados)
	if err != nil {
		return false, 0, err
	}

	for d := range diasTocados {
		if err := s.RecomputeDay(ctx, d.trabajador, d.date); err != nil {
			s.log.Error("recálculo tras empuje", "trabajador", d.trabajador, "error", err)
		}
	}

	return nuevo, points, nil
}

// findSource locates the folder the file belongs to, registering it the first time
// if the push says which account it came from.
//
// It used to refuse to create it, on the grounds that an unknown folder is usually
// an n8n misconfiguration. That reasoning does not survive contact with how this is
// actually set up: every shared folder IS a seller's GPS profile, so a folder
// nobody has seen before is a new seller, not a mistake — and there are dozens.
// Refusing meant somebody had to register 53 folders by hand before a single file
// could get in, and one more every time a seller changed tablet.
//
// What stops it from being a phantom source is the account: it names the branch,
// so the folder is born with its branch already set. Without an account it still
// refuses, because then there really is nothing to attach it to.
func (s *Service) findSource(ctx context.Context, e Pushed) (store.DriveSource, error) {
	if e.SourceID != "" {
		f, err := s.q.SourceByID(ctx, e.SourceID)
		if err != nil {
			return store.DriveSource{}, fmt.Errorf("la source %s no existe", e.SourceID)
		}
		return f, nil
	}

	if e.FolderID == "" {
		return store.DriveSource{}, fmt.Errorf("hay que decir de qué carpeta viene (fuenteId o folderId)")
	}

	fuentes, err := s.q.ActiveSources(ctx)
	if err != nil {
		return store.DriveSource{}, err
	}
	for _, f := range fuentes {
		if f.FolderID == e.FolderID {
			return f, nil
		}
	}

	if e.Account == "" {
		return store.DriveSource{}, fmt.Errorf(
			"la carpeta %s no está dada de alta y el empuje no dice de qué cuenta viene", e.FolderID)
	}

	// El nombre de la carpeta es el del perfil de GPS, o sea el del vendedor.
	nombre := e.FolderID
	if len(e.FolderPath) > 0 && e.FolderPath[len(e.FolderPath)-1] != "" {
		nombre = e.FolderPath[len(e.FolderPath)-1]
	}

	branchID, err := s.branchOfAccount(ctx, e.Account)
	if err != nil {
		return store.DriveSource{}, err
	}

	return s.q.CreateSource(ctx, store.CreateSourceParams{
		ID: newID(), Name: nombre, FolderID: e.FolderID,
		Type: store.SourceSeller, BranchID: &branchID, Credential: e.Account,
	})
}

// mismaCuenta: si la carpeta ya está en la sucursal de esa cuenta, no hay nada que
// hacer. Se compara por sucursal y no por el texto de la credencial, porque la
// misma sucursal puede escribirse de varias formas ("camaguey.procovar@…" y
// "camagueyprocovar@…").
func (s *Service) mismaCuenta(source store.DriveSource, cuenta string) bool {
	if source.BranchID == nil || *source.BranchID == "" {
		return false
	}
	suc, err := s.q.BranchByName(context.Background(), nombreDeCuenta(cuenta))
	return err == nil && suc.ID == *source.BranchID
}

// reasignarSucursal pone la carpeta en su sucursal y arrastra lo que ya entró por
// ella: sus ficheros, los vendedores que salieron de su nombre y los días de esos
// vendedores. Sin eso habría que borrar y volver a ingerir, y los ficheros ya están
// apartados en Drive: no volverían a entrar.
func (s *Service) reasignarSucursal(ctx context.Context, source *store.DriveSource, cuenta string) error {
	branchID, err := s.branchOfAccount(ctx, cuenta)
	if err != nil {
		return err
	}
	if err := s.q.SetSourceBranch(ctx, store.SetSourceBranchParams{
		ID: source.ID, BranchID: &branchID, Credential: cuenta,
	}); err != nil {
		return err
	}
	if err := s.q.MoveSourceDataToBranch(ctx, store.MoveSourceDataToBranchParams{
		SourceID: source.ID, BranchID: branchID,
	}); err != nil {
		return err
	}
	source.BranchID = &branchID
	source.Credential = cuenta
	return nil
}

// sourcePorCarpeta busca la carpeta por su id de Drive, sin darla de alta. Se usa
// para enterarse de su sucursal sin el efecto secundario de crear fuentes nuevas.
func (s *Service) sourcePorCarpeta(ctx context.Context, folderID string) (store.DriveSource, bool) {
	fuentes, err := s.q.ActiveSources(ctx)
	if err != nil {
		return store.DriveSource{}, false
	}
	for _, f := range fuentes {
		if f.FolderID == folderID {
			return f, true
		}
	}
	return store.DriveSource{}, false
}

// AssignFolderBranch pone una carpeta en la sucursal de la cuenta que la comparte,
// y arrastra consigo lo que ya había entrado por ella. Devuelve el nombre de la
// sucursal, o vacío si la carpeta no está dada de alta.
func (s *Service) AssignFolderBranch(ctx context.Context, folderID, cuenta string) (string, error) {
	fuente, ok := s.sourcePorCarpeta(ctx, folderID)
	if !ok {
		return "", nil
	}
	if s.mismaCuenta(fuente, cuenta) {
		return nombreDeCuenta(cuenta), nil
	}
	if err := s.reasignarSucursal(ctx, &fuente, cuenta); err != nil {
		return "", err
	}
	return nombreDeCuenta(cuenta), nil
}
