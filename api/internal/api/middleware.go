// Package api is the panel's HTTP server.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/procovar/procovar-rutas/api/internal/auth"
	"github.com/procovar/procovar-rutas/api/internal/scope"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// SessionCookie is the one procovar-auth sets and every application under the
// same root domain shares.
const CookieSesion = "qb.session_token"

type ctxKey string

const claveIdentidad ctxKey = "identidad"

// Caller is the identity of whoever is asking, already resolved against the local database.
type Caller struct {
	auth.Identity
	// SellerID is their local record. Empty when they have none: a super admin who
	// is not a seller does not appear in the sellers table.
	SellerID string
	BranchID string
	// Terms are this person's supervisions, used to compute scope against the date
	// being queried.
	Terms []scope.Term
}

// Scope computes the filter for a date.
func (c *Caller) Scope(fecha time.Time) (scope.Filter, error) {
	return scope.Compute(
		c.Identity.ScopeOf(c.SellerID, c.BranchID), fecha, c.Terms)
}

// WithSession requires a valid procovar-auth session and resolves the identity.
//
// The role is NOT taken from any header or parameter: it always comes from
// verify-session. A role that arrives from the client is a role the client picks.
func (s *Server) WithSession(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieSesion)
		if err != nil || cookie.Value == "" {
			respondError(w, http.StatusUnauthorized, "sin sesión")
			return
		}

		sesion, err := s.auth.VerifySession(r.Context(), cookie.Value)
		if err != nil {
			s.log.Debug("sesión rechazada", "error", err)
			respondError(w, http.StatusUnauthorized, "sesión no válida")
			return
		}

		identidad := sesion.Translate()

		// Quien entra es quien tiene la llave de entrada, no quien tenga un rol que
		// esta aplicación reconozca. Los permisos los reparte Accesos, y así quitarle
		// Rutas a alguien es desmarcar una casilla allí y no tocar código aquí.
		if !identidad.Puede(PermEntrar) {
			respondError(w, http.StatusForbidden, "sin permiso: "+PermEntrar)
			return
		}
		if identidad.Role == "" {
			// Con permiso pero sin rol conocido no se puede calcular qué vendedores le
			// tocan, y enseñárselos todos sería peor que no enseñar nada.
			//
			// El mensaje dice QUÉ ROLES llegaron. Sin eso, "no tiene un rol con
			// acceso" es indistinguible de "tiene uno que aquí no se reconoce", y son
			// dos arreglos distintos en dos sitios distintos.
			s.log.Warn("sesión sin rol reconocible",
				"usuario", identidad.Email, "role", sesion.Rol, "rbac.roles", sesion.Rbac.Roles)
			respondError(w, http.StatusForbidden,
				"este usuario no tiene un rol que rutas reconozca (recibido: "+
					strings.Join(append([]string{sesion.Rol}, sesion.Rbac.Roles...), ", ")+")")
			return
		}

		ctx, err := s.resolveCaller(r.Context(), identidad)
		if err != nil {
			s.log.Error("resolviendo identidad", "usuario", identidad.AuthUserID, "error", err)
			respondError(w, http.StatusInternalServerError, "no se pudo resolver la identidad")
			return
		}

		siguiente.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claveIdentidad, ctx)))
	})
}

// resolveCaller crosses the procovar-auth identity with the local database.
func (s *Server) resolveCaller(ctx context.Context, id auth.Identity) (*Caller, error) {
	c := &Caller{Identity: id}

	trab, err := s.q.SellerByAuthID(ctx, &id.AuthUserID)
	switch {
	case err == nil:
		c.SellerID = trab.ID
		c.BranchID = trab.BranchID
	case errors.Is(err, pgx.ErrNoRows):
		// No local record. Normal for a super admin or for office staff.
	default:
		return nil, err
	}

	// The session's branch wins over the record's: it is the one the person
	// eligió en procovar-auth.
	if id.AuthOrgID != "" {
		if suc, err := s.q.BranchByAuthOrg(ctx, &id.AuthOrgID); err == nil {
			c.BranchID = suc.ID
		} else if id.AuthOrgNombre != "" {
			// Todavía no están atadas. Las sucursales de aquí nacieron del nombre de
			// la cuenta de Drive ("Bayamo") y las de Accesos se crearon a mano
			// ("Granma"): son la misma y no había nada que las uniera, así que quien
			// entraba se quedaba sin sucursal —y una supervisora sin sucursal no ve a
			// nadie.
			//
			// Se cruzan por la clave, y al encontrarla se atan: la próxima vez la
			// búsqueda es directa y esto no vuelve a correr.
			if suc, err := s.q.BranchByKey(ctx, claveDeSucursal(id.AuthOrgNombre)); err == nil {
				c.BranchID = suc.ID
				org := id.AuthOrgID
				if err := s.q.LinkBranchToAuthOrg(ctx, store.LinkBranchToAuthOrgParams{
					ID: suc.ID, AuthOrgID: &org,
				}); err != nil {
					s.log.Warn("no se pudo atar la sucursal con Accesos",
						"sucursal", suc.Name, "org", id.AuthOrgNombre, "error", err)
				}
			}
		}
	}

	if c.SellerID != "" {
		filas, err := s.q.SupervisorTerms(ctx, c.SellerID)
		if err != nil {
			return nil, err
		}
		for _, v := range filas {
			c.Terms = append(c.Terms, scope.Term{
				ManagerID:    v.ManagerID,
				SupervisorID: v.SupervisorID,
				From:         v.From,
				To:           v.To,
			})
		}
	}

	return c, nil
}

// AdminOnly guards what only super admins and administrators may touch.
// AdminOnly queda para lo que no tiene llave propia todavía. Lo que sí la tiene se
// exige con Exige(...), que es lo que permite repartirlo desde Accesos.
func AdminOnly(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r)
		if c == nil || !c.Puede(PermAdministracion) {
			respondError(w, http.StatusForbidden, "sin permiso: "+PermAdministracion)
			return
		}
		siguiente.ServeHTTP(w, r)
	})
}

// FromContext pulls the identity out of the request.
func FromContext(r *http.Request) *Caller {
	c, _ := r.Context().Value(claveIdentidad).(*Caller)
	return c
}

// --- respuestas -------------------------------------------------------------

func respond(w http.ResponseWriter, estado int, cuerpo any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	if cuerpo != nil {
		_ = json.NewEncoder(w).Encode(cuerpo)
	}
}

func respondError(w http.ResponseWriter, estado int, mensaje string) {
	respond(w, estado, map[string]string{"error": mensaje})
}

// scopeParams turns the filter into the three parameters every query expects.
// queries in the panel.
type scopeParams struct {
	BranchID string
	Sellers  []string
	Exclude  string
	Empty    bool
}

func fromFilter(f scope.Filter) scopeParams {
	p := scopeParams{
		BranchID: f.BranchID,
		Sellers:  f.SellersIn,
		Exclude:  f.SellerNot,
		Empty:    f.Empty,
	}
	if p.Sellers == nil {
		p.Sellers = []string{}
	}
	return p
}
