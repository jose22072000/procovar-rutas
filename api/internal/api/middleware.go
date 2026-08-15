// Package api es el servidor HTTP del panel.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/procovar/procovar-rutas/api/internal/alcance"
	"github.com/procovar/procovar-rutas/api/internal/auth"
)

// CookieSesion es la que pone procovar-auth y comparten todas las aplicaciones
// bajo el mismo dominio raíz.
const CookieSesion = "qb.session_token"

type claveCtx string

const claveIdentidad claveCtx = "identidad"

// Contexto es la identidad de quien pide, ya resuelta contra la base local.
type Contexto struct {
	auth.Identidad
	// TrabajadorID es su ficha local. Vacío si no tiene: un super admin que no
	// es vendedor no aparece en la tabla de trabajadores.
	TrabajadorID string
	SucursalID   string
	// Vigencias son las supervisiones de esta persona, para calcular el alcance
	// contra la fecha consultada.
	Vigencias []alcance.Vigencia
}

// Alcance calcula el filtro para una fecha.
func (c *Contexto) Alcance(fecha time.Time) (alcance.Filtro, error) {
	return alcance.Calcular(
		c.Identidad.AlcanceDe(c.TrabajadorID, c.SucursalID), fecha, c.Vigencias)
}

// ConSesion exige una sesión válida de procovar-auth y resuelve la identidad.
//
// El rol NO se acepta de ninguna cabecera ni parámetro: siempre sale de
// verify-session. Un rol que llegue del cliente es un rol que el cliente elige.
func (s *Servidor) ConSesion(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieSesion)
		if err != nil || cookie.Value == "" {
			responderError(w, http.StatusUnauthorized, "sin sesión")
			return
		}

		sesion, err := s.auth.VerificarSesion(r.Context(), cookie.Value)
		if err != nil {
			s.log.Debug("sesión rechazada", "error", err)
			responderError(w, http.StatusUnauthorized, "sesión no válida")
			return
		}

		identidad := sesion.Traducir()
		if identidad.Rol == "" {
			responderError(w, http.StatusForbidden, "este usuario no tiene un rol con acceso al panel de rutas")
			return
		}
		if identidad.Rol == alcance.RolGestor {
			// Se corta aquí y no en cada manejador: el vendedor no entra, y el
			// mensaje explica por qué en vez de dar una pantalla vacía.
			responderError(w, http.StatusForbidden,
				"el panel de rutas es para supervisión; los vendedores no tienen acceso")
			return
		}

		ctx, err := s.resolverContexto(r.Context(), identidad)
		if err != nil {
			s.log.Error("resolviendo identidad", "usuario", identidad.AuthUserID, "error", err)
			responderError(w, http.StatusInternalServerError, "no se pudo resolver la identidad")
			return
		}

		siguiente.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claveIdentidad, ctx)))
	})
}

// resolverContexto cruza la identidad de procovar-auth con la base local.
func (s *Servidor) resolverContexto(ctx context.Context, id auth.Identidad) (*Contexto, error) {
	c := &Contexto{Identidad: id}

	trab, err := s.q.TrabajadorPorAuthID(ctx, &id.AuthUserID)
	switch {
	case err == nil:
		c.TrabajadorID = trab.ID
		c.SucursalID = trab.SucursalID
	case errors.Is(err, pgx.ErrNoRows):
		// No tiene ficha local. Normal en un super admin o en alguien de oficina.
	default:
		return nil, err
	}

	// La sucursal de la sesión manda sobre la de la ficha: es la que la persona
	// eligió en procovar-auth.
	if id.AuthOrgID != "" {
		if suc, err := s.q.SucursalPorAuthOrg(ctx, &id.AuthOrgID); err == nil {
			c.SucursalID = suc.ID
		}
	}

	if c.TrabajadorID != "" {
		filas, err := s.q.VigenciasDeSupervisor(ctx, c.TrabajadorID)
		if err != nil {
			return nil, err
		}
		for _, v := range filas {
			c.Vigencias = append(c.Vigencias, alcance.Vigencia{
				GestorID:     v.GestorID,
				SupervisorID: v.SupervisorID,
				Desde:        v.Desde,
				Hasta:        v.Hasta,
			})
		}
	}

	return c, nil
}

// SoloAdmin protege lo que solo pueden tocar super admin y administradores.
func SoloAdmin(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := DeContexto(r)
		if c == nil || !alcance.PuedeAdministrar(c.Rol) {
			responderError(w, http.StatusForbidden, "hace falta ser administrador")
			return
		}
		siguiente.ServeHTTP(w, r)
	})
}

// DeContexto saca la identidad de la petición.
func DeContexto(r *http.Request) *Contexto {
	c, _ := r.Context().Value(claveIdentidad).(*Contexto)
	return c
}

// --- respuestas -------------------------------------------------------------

func responder(w http.ResponseWriter, estado int, cuerpo any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(estado)
	if cuerpo != nil {
		_ = json.NewEncoder(w).Encode(cuerpo)
	}
}

func responderError(w http.ResponseWriter, estado int, mensaje string) {
	responder(w, estado, map[string]string{"error": mensaje})
}

// paramsAlcance traduce el filtro a los tres parámetros que esperan todas las
// consultas del panel.
type paramsAlcance struct {
	SucursalID   string
	Trabajadores []string
	Excluir      string
	Vacio        bool
}

func deFiltro(f alcance.Filtro) paramsAlcance {
	p := paramsAlcance{
		SucursalID:   f.SucursalID,
		Trabajadores: f.TrabajadoresIn,
		Excluir:      f.TrabajadorNot,
		Vacio:        f.Vacio,
	}
	if p.Trabajadores == nil {
		p.Trabajadores = []string{}
	}
	return p
}
