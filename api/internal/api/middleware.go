// Package api is the panel's HTTP server.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/procovar/procovar-rutas/api/internal/auth"
	"github.com/procovar/procovar-rutas/api/internal/scope"
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
		if identidad.Role == "" {
			respondError(w, http.StatusForbidden, "este usuario no tiene un rol con acceso al panel de rutas")
			return
		}
		if identidad.Role == scope.RoleAgent {
			// Cut off here and not in every handler: the seller does not come in, and
			// the message explains why instead of showing an empty screen.
			respondError(w, http.StatusForbidden,
				"el panel de rutas es para supervisión; los sellers no tienen acceso")
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
func AdminOnly(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r)
		if c == nil || !scope.CanAdminister(c.Role) {
			respondError(w, http.StatusForbidden, "hace falta ser administrador")
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
