package api

import (
	"encoding/json"
	"fmt"
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

// Persona es alguien de Accesos: una cuenta de verdad, con su rol.
//
// Es lo que va en el desplegable de «¿de quién es este GPS?». Antes ahí salían los
// `trabajador` de esta base, que se habían creado solos CON EL NOMBRE DE LA CARPETA:
// asignar la carpeta «ALEXANDER» al trabajador «ALEXANDER» era atarla a sí misma y no
// enganchaba con nadie.
type Persona struct {
	AuthUserID string   `json:"authUserId"`
	Name       string   `json:"name"`
	Email      string   `json:"email"`
	Branch     string   `json:"branch"`
	Roles      []string `json:"roles"`
	// SellerID: si esta persona ya tiene ficha aquí, cuál. Sirve para saber quién
	// está ya enganchado sin tener que cruzar dos listas en la pantalla.
	SellerID string `json:"sellerId"`
}

// GET /api/personas — la gente de Accesos que puede llevar un GPS.
//
// Gestores y supervisores: son los que salen a la calle. Un administrador o un
// gerente no lleva teléfono de ruta, y meterlos en el desplegable es alargar una
// lista que se usa para elegir deprisa.
func (s *Server) personas(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	// Un super admin ve las de todas las sucursales; el resto, las de la suya.
	org := ""
	if c.Role != "super_admin" {
		if suc, err := s.q.BranchByID(r.Context(), c.BranchID); err == nil && suc.AuthOrgID != nil {
			org = *suc.AuthOrgID
		}
	}

	gente, err := s.auth.Members(r.Context(), org, []string{"GESTOR", "SUPERVISOR"})
	if err != nil {
		// Que Accesos no conteste no puede dejar la pantalla muda: se dice qué pasó,
		// porque sin esta lista no se puede asignar nada y hay que saber por qué.
		s.log.Error("no se pudo traer la gente de Accesos", "error", err)
		respondError(w, http.StatusBadGateway,
			"Accesos no contestó, así que no hay lista de personas: "+err.Error())
		return
	}

	out := make([]Persona, 0, len(gente))
	for _, m := range gente {
		p := Persona{
			AuthUserID: m.ID,
			Name:       m.Name,
			Email:      m.Email,
			Branch:     m.OrganizationName,
			Roles:      m.Roles,
		}
		// Si ya tiene ficha aquí, se dice: es la diferencia entre «esta persona ya
		// está enganchada» y «esta persona todavía no ha aparecido».
		if t, err := s.q.SellerByAuthID(r.Context(), &m.ID); err == nil {
			p.SellerID = t.ID
		}
		out = append(out, p)
	}
	respond(w, http.StatusOK, out)
}

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
	DaysSilent int32  `json:"daysSilent"`
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
		// De Accesos: es una PERSONA. Se admite todavía `sellerId` para no romper
		// nada que lo estuviera usando, pero lo bueno es `authUserId`.
		AuthUserID string `json:"authUserId"`
		Name       string `json:"name"`
		SellerID   string `json:"sellerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, "cuerpo ilegible")
		return
	}
	if p.AuthUserID == "" && p.SellerID == "" {
		respondError(w, http.StatusBadRequest, "falta la persona")
		return
	}

	fuente, err := s.q.SourceByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "esa carpeta no está")
		return
	}

	trab, err := s.resolverPersona(r, fuente, p.AuthUserID, p.Name, p.SellerID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
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

// resolverPersona devuelve la ficha local de esa persona, creándola o atándola.
//
// # El caso que importa
//
// La carpeta «ALEXANDER» ya tiene colgado un trabajador que se creó SOLO, con el
// nombre de la carpeta, y de él cuelgan sus dos mil días de recorrido. Cuando alguien
// dice por fin que esa carpeta es de una persona de Accesos, lo que hay que hacer es
// ATAR ese trabajador a esa cuenta — no crear una persona nueva. Si se creara, la
// historia se quedaría en el muñeco viejo y la persona de verdad empezaría en blanco.
//
// Por eso el orden es: ¿ya hay alguien con esa cuenta? Si no, ¿la carpeta ya tenía
// dueño? Entonces ese, atado. Y sólo si no hay ni lo uno ni lo otro, uno nuevo.
func (s *Server) resolverPersona(
	r *http.Request,
	fuente store.DriveSource,
	authUserID, nombre, sellerID string,
) (store.Seller, error) {
	ctx := r.Context()
	c := FromContext(r)

	// Camino viejo: se dijo directamente qué ficha usar.
	if authUserID == "" {
		t, err := s.q.SellerByID(ctx, sellerID)
		if err != nil {
			return store.Seller{}, fmt.Errorf("ese vendedor no existe")
		}
		if c.Role != "super_admin" && c.BranchID != "" && t.BranchID != c.BranchID {
			return store.Seller{}, fmt.Errorf("sin acceso a ese vendedor")
		}
		return t, nil
	}

	// 1. ¿Esa cuenta ya tiene ficha aquí?
	if t, err := s.q.SellerByAuthID(ctx, &authUserID); err == nil {
		return t, nil
	}

	// La sucursal: la de la carpeta, y si no la tiene, la de quien está asignando.
	sucursalID := c.BranchID
	if fuente.BranchID != nil && *fuente.BranchID != "" {
		sucursalID = *fuente.BranchID
	}
	if sucursalID == "" {
		return store.Seller{}, fmt.Errorf(
			"esa carpeta todavía no tiene sucursal: entrará sola con su primer fichero")
	}

	// 2. ¿La carpeta ya tenía un trabajador? Ese, atado a la cuenta — con su historia.
	if fuente.SellerID != nil && *fuente.SellerID != "" {
		if err := s.q.LinkSellerToAuth(ctx, store.LinkSellerToAuthParams{
			ID: *fuente.SellerID, AuthUserID: &authUserID, Name: nombre,
		}); err != nil {
			return store.Seller{}, fmt.Errorf("atando a la cuenta: %w", err)
		}
		return s.q.SellerByID(ctx, *fuente.SellerID)
	}

	// 3. Y si la carpeta no tenía dueño, uno nuevo con su cuenta puesta desde el
	//    principio.
	if nombre == "" {
		nombre = fuente.Name
	}
	t, err := s.q.CreateSellerFromAuth(ctx, store.CreateSellerFromAuthParams{
		ID: newID(), Name: nombre, BranchID: sucursalID, AuthUserID: &authUserID,
	})
	if err != nil {
		return store.Seller{}, fmt.Errorf("creando la ficha: %w", err)
	}
	return t, nil
}
