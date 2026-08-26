package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Los GPS, que son las carpetas de Drive.
//
// # Por qué se asigna la CARPETA y no un alias
//
// Cada carpeta compartida de Drive ES el perfil de un teléfono: se llama «GPS Diana
// Acosta», «STGGari» o «TABLET3», y dentro están sus `AAAAMMDD.gpx`. Eso ya dice de
// quién es el GPS.
//
// La primera versión de esto pedía teclear un ALIAS por dispositivo, a mano y de uno
// en uno, y encima solo se podía cuando ya se había atascado un fichero suyo. Era
// hacer a mano, cincuenta y tres veces, lo que la carpeta lleva escrito en el nombre
// — y para colmo el alias solo arregla el fichero SIGUIENTE, no los que ya estaban
// esperando.
//
// Asignar la carpeta hace las tres cosas de una vez: la marca como de esa persona
// (con lo que la primera regla de resolución acierta sin mirar nada más), recuerda su
// nombre como alias (por si sus ficheros aparecen algún día por otro sitio) y coloca
// TODOS los ficheros que se habían quedado sin dueño en ella, recalculando sus días.

// Gps es una carpeta de Drive: un teléfono, y lo que ha traído.
type Gps struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	FolderID string `json:"folderId"`
	Type     string `json:"type"`
	Branch   string `json:"branch"`
	// SellerID vacío = todavía no se ha dicho de quién es este teléfono.
	SellerID string `json:"sellerId"`
	Seller   string `json:"seller"`
	Files    int64  `json:"files"`
	LastFile string `json:"lastFile"`
	// DaysSilent: -1 = por esa carpeta no ha entrado nunca nada.
	DaysSilent int32 `json:"daysSilent"`
	LastError  string `json:"lastError"`
}

// GET /api/gps — todas las carpetas, con de quién son y qué han traído.
func (s *Server) listGps(w http.ResponseWriter, r *http.Request) {
	filas, err := s.q.ActiveSourcesWithBranch(r.Context())
	if err != nil {
		s.fail(w, "carpetas", err)
		return
	}

	c := FromContext(r)
	out := make([]Gps, 0, len(filas))
	for _, f := range filas {
		// Un gerente ve las de su sucursal. Las que todavía no tienen sucursal salen
		// para todos: son precisamente las que hay que colocar, y esconderlas hasta
		// que alguien las coloque es un círculo cerrado.
		if c.Role != "super_admin" && c.BranchID != "" &&
			f.BranchID != nil && *f.BranchID != c.BranchID {
			continue
		}

		g := Gps{
			ID:         f.ID,
			Name:       f.Name,
			FolderID:   f.FolderID,
			Type:       string(f.Type),
			Branch:     f.Branch,
			Seller:     f.Seller,
			Files:      f.Ficheros,
			LastFile:   f.Ultima,
			DaysSilent: f.DiasCallado,
		}
		if f.SellerID != nil {
			g.SellerID = *f.SellerID
		}
		if f.LastError != nil {
			g.LastError = *f.LastError
		}
		out = append(out, g)
	}
	respond(w, http.StatusOK, out)
}

// POST /api/gps/{id}/asignar — decir de quién es este teléfono.
//
// Lo que hace, en una sola operación:
//
//  1. Marca la carpeta como de esa persona. A partir de ahí, todo lo que entre por
//     ella se resuelve sin mirar nada más.
//  2. Recuerda su nombre como alias, por si sus ficheros aparecen por otro camino
//     (una carpeta nueva, un empuje suelto de n8n).
//  3. Y COLOCA lo que ya estaba esperando: los ficheros de esa carpeta que se
//     quedaron sin dueño se asignan y sus días se recalculan. Son los días que
//     llevaban en la bandeja esperando a que alguien dijera quién era.
func (s *Server) assignGps(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)
	id := chi.URLParam(r, "id")

	var p struct {
		SellerID string `json:"sellerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, "cuerpo ilegible")
		return
	}
	if p.SellerID == "" {
		respondError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}

	fuente, err := s.q.SourceByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "esa carpeta no está")
		return
	}
	trab, err := s.q.SellerByID(r.Context(), p.SellerID)
	if err != nil {
		respondError(w, http.StatusNotFound, "ese vendedor no existe")
		return
	}
	// Nadie asigna una carpeta a alguien de otra sucursal.
	if c.Role != "super_admin" && c.BranchID != "" && trab.BranchID != c.BranchID {
		respondError(w, http.StatusForbidden, "sin acceso a ese vendedor")
		return
	}

	if err := s.q.SetSourceSeller(r.Context(), store.SetSourceSellerParams{
		ID: fuente.ID, SellerID: &trab.ID, BranchID: &trab.BranchID,
	}); err != nil {
		s.fail(w, "asignando la carpeta", err)
		return
	}

	// El nombre de la carpeta, recordado como alias. Cuesta una fila y cubre el caso
	// de que mañana los mismos ficheros lleguen por otro camino.
	if _, err := s.q.CreateAlias(r.Context(), store.CreateAliasParams{
		ID:            newID(),
		Alias:         gpx.Normalize(fuente.Name),
		OriginalAlias: fuente.Name,
		SellerID:      trab.ID,
		BranchID:      &trab.BranchID,
		CreatedBy:     &c.AuthUserID,
	}); err != nil {
		s.log.Warn("no se pudo recordar el nombre de la carpeta", "carpeta", fuente.Name, "error", err)
	}

	// Y lo que estaba esperando. Esto es lo que hacía falta y no había: asignar un
	// alias arreglaba el fichero siguiente y dejaba los anteriores en la bandeja.
	colocados := 0
	pendientes, err := s.q.UnassignedFilesOfSource(r.Context(), fuente.ID)
	if err != nil {
		s.log.Warn("leyendo los ficheros sin dueño de la carpeta", "error", err)
	}
	dias := map[string]bool{}
	for _, f := range pendientes {
		fila, err := s.q.AssignFile(r.Context(), store.AssignFileParams{
			ID: f.ID, SellerID: &trab.ID, BranchID: &trab.BranchID, Date: f.Date,
		})
		if err != nil {
			s.log.Warn("colocando fichero", "fichero", f.ID, "error", err)
			continue
		}
		colocados++
		// Los días se recalculan UNA vez cada uno, aunque los toquen varios ficheros:
		// una carpeta con doscientos días atrasados haría doscientos recálculos
		// repetidos.
		if fila.Date != nil {
			dias[fila.Date.Format(iso)] = true
		}
	}
	for d := range dias {
		fecha, err := parseDate(d)
		if err != nil {
			continue
		}
		if err := s.ingest.RecomputeDay(r.Context(), trab.ID, fecha); err != nil {
			s.log.Error("recálculo tras asignar la carpeta", "dia", d, "error", err)
		}
	}

	s.auth.RecordAudit(r.Context(), "rutas.gps.asignar", fuente.ID, c.AuthUserID)
	s.notify(r, events.Event{Type: events.TypeFile, Detail: "carpeta asignada"})
	respond(w, http.StatusOK, map[string]any{
		"ok": true,
		// Cuántos días se colocaron con esto: es la respuesta a «¿sirvió de algo?».
		"placed": colocados,
		"days":   len(dias),
	})
}
