// Package api es el servidor HTTP del panel.
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

// CookieSesion es la que pone procovar-auth y comparten todas las aplicaciones
// bajo el mismo dominio raíz.
const CookieSesion = "qb.session_token"

type ctxKey string

const claveIdentidad ctxKey = "identidad"

// Caller es la identidad de quien pide, ya resuelta contra la base local.
type Caller struct {
	auth.Identity
	// SellerID es su ficha local. Vacío si no tiene: un super admin que no
	// es vendedor no aparece en la tabla de trabajadores.
	SellerID string
	BranchID string
	// Terms son las supervisiones de esta persona, para calcular el alcance
	// contra la fecha consultada.
	Terms []scope.Term
}

// Scope calcula el filtro para una fecha.
func (c *Caller) Scope(fecha time.Time) (scope.Filter, error) {
	return scope.Compute(
		c.Identity.AlcanceDe(c.SellerID, c.BranchID), fecha, c.Terms)
}

// WithSession exige una sesión válida de procovar-auth y resuelve la identidad.
//
// El rol NO se acepta de ninguna cabecera ni parámetro: siempre sale de
// verify-session. Un rol que llegue del cliente es un rol que el cliente elige.
func (s *Server) WithSession(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieSesion)
		if err != nil || cookie.Value == "" {
			respondError(w, http.StatusUnauthorized, "sin sesión")
			return
		}

		sesion, err := s.auth.VerificarSesion(r.Context(), cookie.Value)
		if err != nil {
			s.log.Debug("sesión rechazada", "error", err)
			respondError(w, http.StatusUnauthorized, "sesión no válida")
			return
		}

		identidad := sesion.Traducir()
		if identidad.Role == "" {
			respondError(w, http.StatusForbidden, "este usuario no tiene un rol con acceso al panel de rutas")
			return
		}
		if identidad.Role == scope.RoleAgent {
			// Se corta aquí y no en cada manejador: el vendedor no entra, y el
			// mensaje explica por qué en vez de dar una pantalla vacía.
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

// resolveCaller cruza la identidad de procovar-auth con la base local.
func (s *Server) resolveCaller(ctx context.Context, id auth.Identity) (*Caller, error) {
	c := &Caller{Identity: id}

	trab, err := s.q.SellerByAuthID(ctx, &id.AuthUserID)
	switch {
	case err == nil:
		c.SellerID = trab.ID
		c.BranchID = trab.BranchID
	case errors.Is(err, pgx.ErrNoRows):
		// No tiene ficha local. Normal en un super admin o en alguien de oficina.
	default:
		return nil, err
	}

	// La sucursal de la sesión manda sobre la de la ficha: es la que la persona
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

// AdminOnly protege lo que solo pueden tocar super admin y administradores.
func AdminOnly(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r)
		if c == nil || !scope.PuedeAdministrar(c.Role) {
			respondError(w, http.StatusForbidden, "hace falta ser administrador")
			return
		}
		siguiente.ServeHTTP(w, r)
	})
}

// FromContext saca la identidad de la petición.
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

// scopeParams traduce el filtro a los tres parámetros que esperan todas las
// consultas del panel.
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
