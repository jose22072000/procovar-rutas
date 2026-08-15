package api

import (
	"encoding/json"
	"github.com/procovar/procovar-rutas/api/internal/events"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// The inbox: the files ingest could not assign or date, and the ones that
// failed. It is what guarantees no file is ever lost in silence.
func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	filtro, err := c.Scope(time.Now())
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}

	filas, err := s.q.Inbox(r.Context(), store.InboxParams{
		BranchID:  filtro.BranchID,
		LimitRows: 200,
	})
	if err != nil {
		s.fail(w, "inbox", err)
		return
	}
	respond(w, http.StatusOK, aInboxFiles(filas))
}

type assignRequest struct {
	FileID   string `json:"fileId"`
	SellerID string `json:"sellerId"`
	Date     string `json:"date"`
	// RememberAlias makes the next one resolve on its own. It is what turns the
	// inbox into work done once per device instead of every single day.
	//
	RememberAlias bool   `json:"rememberAlias"`
	Alias         string `json:"alias"`
}

func (s *Server) assign(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	var p assignRequest
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, "cuerpo inválido")
		return
	}
	if p.FileID == "" || p.SellerID == "" {
		respondError(w, http.StatusBadRequest, "faltan el fichero o el vendedor")
		return
	}

	trab, err := s.q.SellerByID(r.Context(), p.SellerID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ese vendedor no existe")
		return
	}
	// An administrator cannot assign a file to a seller from another branch: that
	// would be slipping data in where they have no business looking.
	if c.Role == "admin" && c.BranchID != "" && trab.BranchID != c.BranchID {
		respondError(w, http.StatusForbidden, "ese vendedor es de otra sucursal")
		return
	}

	var fecha *time.Time
	if p.Date != "" {
		f, err := time.Parse(iso, p.Date)
		if err != nil {
			respondError(w, http.StatusBadRequest, "fecha inválida")
			return
		}
		fecha = &f
	}

	fila, err := s.q.AssignFile(r.Context(), store.AssignFileParams{
		ID:       p.FileID,
		SellerID: &p.SellerID,
		BranchID: &trab.BranchID,
		Date:     fecha,
	})
	if err != nil {
		s.fail(w, "asignando fichero", err)
		return
	}

	if p.RememberAlias {
		alias := p.Alias
		if alias == "" && fila.AliasHint != nil {
			alias = *fila.AliasHint
		}
		if alias != "" {
			if _, err := s.q.CreateAlias(r.Context(), store.CreateAliasParams{
				ID:            newID(),
				Alias:         gpx.Normalize(alias),
				OriginalAlias: alias,
				SellerID:      p.SellerID,
				BranchID:      &trab.BranchID,
				CreatedBy:     &c.AuthUserID,
			}); err != nil {
				s.log.Warn("no se pudo recordar el alias", "alias", alias, "error", err)
			}
		}
	}

	// The points are already in the database from ingest: assigning the file only
	// calls for recomputing the day, without downloading anything from Drive again.
	if fila.SellerID != nil && fila.Date != nil {
		if err := s.ingest.RecomputeDay(r.Context(), *fila.SellerID, *fila.Date); err != nil {
			s.log.Error("recálculo tras assign", "error", err)
		}
	}

	s.auth.RecordAudit(r.Context(), "rutas.fichero.assign", fila.ID, c.AuthUserID)
	// Anyone with the inbox open on another screen will see that file is no longer
	// waiting, without reloading.
	s.notify(r, events.Event{Type: events.TypeFile, Detail: "asignado"})
	respond(w, http.StatusOK, aAssignedFile(fila))
}

func (s *Server) listAliases(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)
	sucursal := ""
	if c.Role != "super_admin" {
		sucursal = c.BranchID
	}
	filas, err := s.q.BranchAliases(r.Context(), sucursal)
	if err != nil {
		s.fail(w, "alias", err)
		return
	}
	respond(w, http.StatusOK, aDeviceAliases(filas))
}

func (s *Server) deleteAlias(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)
	id := chi.URLParam(r, "id")
	if err := s.q.DeleteAlias(r.Context(), id); err != nil {
		s.fail(w, "borrando alias", err)
		return
	}
	s.auth.RecordAudit(r.Context(), "rutas.alias.borrar", id, c.AuthUserID)
	respond(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	filas, err := s.q.ActiveSources(r.Context())
	if err != nil {
		s.fail(w, "sources", err)
		return
	}
	respond(w, http.StatusOK, aDriveSources(filas))
}

type sourceRequest struct {
	Name     string `json:"name"`
	FolderID string `json:"folderId"`
	Type     string `json:"type"`
	BranchID string `json:"branchId"`
	SellerID string `json:"sellerId"`
	// Credential is the Google account this folder is read with.
	Credential string `json:"credential"`
}

func (s *Server) createSource(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	var p sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		respondError(w, http.StatusBadRequest, "cuerpo inválido")
		return
	}
	if p.Name == "" || p.FolderID == "" {
		respondError(w, http.StatusBadRequest, "faltan el nombre o el identificador de la carpeta")
		return
	}
	if p.Type == "" {
		p.Type = "SUCURSAL"
	}

	fila, err := s.q.CreateSource(r.Context(), store.CreateSourceParams{
		ID:         newID(),
		Name:       p.Name,
		FolderID:   p.FolderID,
		Type:       store.SourceType(p.Type),
		BranchID:   optional(p.BranchID),
		SellerID:   optional(p.SellerID),
		Credential: p.Credential,
	})
	if err != nil {
		s.fail(w, "creando fuente", err)
		return
	}

	s.auth.RecordAudit(r.Context(), "rutas.fuente.crear", fila.ID, c.AuthUserID)
	respond(w, http.StatusCreated, fila)
}

// scan launches an ingest by hand from the administration screen.
func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	tipo := r.URL.Query().Get("tipo")
	if tipo == "" {
		tipo = ingest.TipoManual
	}

	res, err := s.ingest.Scan(r.Context(), tipo)
	if err != nil {
		s.fail(w, "barrido", err)
		return
	}
	s.auth.RecordAudit(r.Context(), "rutas.ingest.scan", tipo, c.AuthUserID)
	// A scan changes the inbox and the list of scans at once.
	s.notify(r, events.Event{Type: events.TypeScan, Detail: "terminado"})
	respond(w, http.StatusOK, res)
}

func (s *Server) scans(w http.ResponseWriter, r *http.Request) {
	filas, err := s.q.RecentScans(r.Context(), 50)
	if err != nil {
		s.fail(w, "scans", err)
		return
	}
	respond(w, http.StatusOK, aScanLogs(filas))
}
