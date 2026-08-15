package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/ingesta"
)

// La bandeja: los ficheros que la ingesta no supo asignar o fechar, y los que
// dieron error. Es lo que garantiza que ningún fichero se pierda en silencio.
func (s *Servidor) bandeja(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	filtro, err := c.Alcance(time.Now())
	if err != nil {
		responderError(w, http.StatusForbidden, err.Error())
		return
	}

	filas, err := s.q.Bandeja(r.Context(), almacen.BandejaParams{
		SucursalID: filtro.SucursalID,
		Limite:     200,
	})
	if err != nil {
		s.fallo(w, "bandeja", err)
		return
	}
	responder(w, http.StatusOK, aInboxFiles(filas))
}

type peticionAsignar struct {
	FicheroID    string `json:"fileId"`
	TrabajadorID string `json:"sellerId"`
	Fecha        string `json:"date"`
	// RecordarAlias hace que la próxima vez se resuelva solo. Es lo que
	// convierte la bandeja en trabajo de una vez por dispositivo y no de todos
	// los días.
	RecordarAlias bool   `json:"rememberAlias"`
	Alias         string `json:"alias"`
}

func (s *Servidor) asignar(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	var p peticionAsignar
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		responderError(w, http.StatusBadRequest, "cuerpo inválido")
		return
	}
	if p.FicheroID == "" || p.TrabajadorID == "" {
		responderError(w, http.StatusBadRequest, "faltan el fichero o el vendedor")
		return
	}

	trab, err := s.q.TrabajadorPorID(r.Context(), p.TrabajadorID)
	if err != nil {
		responderError(w, http.StatusBadRequest, "ese vendedor no existe")
		return
	}
	// Un administrador no puede asignar un fichero a un vendedor de otra
	// sucursal: sería colar datos donde no le corresponde mirar.
	if c.Rol == "admin" && c.SucursalID != "" && trab.SucursalID != c.SucursalID {
		responderError(w, http.StatusForbidden, "ese vendedor es de otra sucursal")
		return
	}

	var fecha *time.Time
	if p.Fecha != "" {
		f, err := time.Parse(iso, p.Fecha)
		if err != nil {
			responderError(w, http.StatusBadRequest, "fecha inválida")
			return
		}
		fecha = &f
	}

	fila, err := s.q.AsignarFichero(r.Context(), almacen.AsignarFicheroParams{
		ID:           p.FicheroID,
		TrabajadorID: &p.TrabajadorID,
		SucursalID:   &trab.SucursalID,
		Fecha:        fecha,
	})
	if err != nil {
		s.fallo(w, "asignando fichero", err)
		return
	}

	if p.RecordarAlias {
		alias := p.Alias
		if alias == "" && fila.PistaAlias != nil {
			alias = *fila.PistaAlias
		}
		if alias != "" {
			if _, err := s.q.CrearAlias(r.Context(), almacen.CrearAliasParams{
				ID:            nuevoID(),
				Alias:         gpx.Normalizar(alias),
				AliasOriginal: alias,
				TrabajadorID:  p.TrabajadorID,
				SucursalID:    &trab.SucursalID,
				CreatedBy:     &c.AuthUserID,
			}); err != nil {
				s.log.Warn("no se pudo recordar el alias", "alias", alias, "error", err)
			}
		}
	}

	// Los puntos ya están en la base desde la ingesta: asignar el fichero solo
	// exige recalcular el día, sin volver a bajar nada de Drive.
	if fila.TrabajadorID != nil && fila.Fecha != nil {
		if err := s.ingesta.RecalcularDia(r.Context(), *fila.TrabajadorID, *fila.Fecha); err != nil {
			s.log.Error("recálculo tras asignar", "error", err)
		}
	}

	s.auth.RegistrarAuditoria(r.Context(), "rutas.fichero.asignar", fila.ID, c.AuthUserID)
	responder(w, http.StatusOK, aAssignedFile(fila))
}

func (s *Servidor) listarAlias(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)
	sucursal := ""
	if c.Rol != "super_admin" {
		sucursal = c.SucursalID
	}
	filas, err := s.q.AliasDeSucursal(r.Context(), sucursal)
	if err != nil {
		s.fallo(w, "alias", err)
		return
	}
	responder(w, http.StatusOK, aDeviceAliases(filas))
}

func (s *Servidor) borrarAlias(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)
	id := chi.URLParam(r, "id")
	if err := s.q.BorrarAlias(r.Context(), id); err != nil {
		s.fallo(w, "borrando alias", err)
		return
	}
	s.auth.RegistrarAuditoria(r.Context(), "rutas.alias.borrar", id, c.AuthUserID)
	responder(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Servidor) fuentes(w http.ResponseWriter, r *http.Request) {
	filas, err := s.q.FuentesActivas(r.Context())
	if err != nil {
		s.fallo(w, "fuentes", err)
		return
	}
	responder(w, http.StatusOK, aDriveSources(filas))
}

type peticionFuente struct {
	Nombre       string `json:"name"`
	FolderID     string `json:"folderId"`
	Tipo         string `json:"type"`
	SucursalID   string `json:"branchId"`
	TrabajadorID string `json:"sellerId"`
	// Credencial es la cuenta de Google con la que se lee esta carpeta.
	Credencial string `json:"credential"`
}

func (s *Servidor) crearFuente(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	var p peticionFuente
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		responderError(w, http.StatusBadRequest, "cuerpo inválido")
		return
	}
	if p.Nombre == "" || p.FolderID == "" {
		responderError(w, http.StatusBadRequest, "faltan el nombre o el identificador de la carpeta")
		return
	}
	if p.Tipo == "" {
		p.Tipo = "SUCURSAL"
	}

	fila, err := s.q.CrearFuente(r.Context(), almacen.CrearFuenteParams{
		ID:           nuevoID(),
		Nombre:       p.Nombre,
		FolderID:     p.FolderID,
		Tipo:         almacen.TipoFuente(p.Tipo),
		SucursalID:   opcional(p.SucursalID),
		TrabajadorID: opcional(p.TrabajadorID),
		Credencial:   p.Credencial,
	})
	if err != nil {
		s.fallo(w, "creando fuente", err)
		return
	}

	s.auth.RegistrarAuditoria(r.Context(), "rutas.fuente.crear", fila.ID, c.AuthUserID)
	responder(w, http.StatusCreated, fila)
}

// barrer lanza una ingesta a mano desde la pantalla de administración.
func (s *Servidor) barrer(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	tipo := r.URL.Query().Get("tipo")
	if tipo == "" {
		tipo = ingesta.TipoManual
	}

	res, err := s.ingesta.Barrer(r.Context(), tipo)
	if err != nil {
		s.fallo(w, "barrido", err)
		return
	}
	s.auth.RegistrarAuditoria(r.Context(), "rutas.ingesta.barrer", tipo, c.AuthUserID)
	responder(w, http.StatusOK, res)
}

func (s *Servidor) barridos(w http.ResponseWriter, r *http.Request) {
	filas, err := s.q.UltimosBarridos(r.Context(), 50)
	if err != nil {
		s.fallo(w, "barridos", err)
		return
	}
	responder(w, http.StatusOK, aScanLogs(filas))
}
