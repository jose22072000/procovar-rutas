package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/calendario"
)

const iso = "2006-01-02"

// rangoDeConsulta lee ?desde= y ?hasta=. Sin parámetros, la semana laboral en
// curso, que es lo que se quiere ver al entrar.
func rangoDeConsulta(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	desdeStr, hastaStr := q.Get("desde"), q.Get("hasta")

	if desdeStr == "" && hastaStr == "" {
		semana := calendario.SemanaLaboral(time.Now())
		return semana[0], semana[len(semana)-1], nil
	}

	desde, err := fechaDe(desdeStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	hasta := desde
	if hastaStr != "" {
		hasta, err = fechaDe(hastaStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if hasta.Before(desde) {
		return time.Time{}, time.Time{}, fmt.Errorf("la fecha final es anterior a la inicial")
	}
	// Un rango sin tope permitiría pedir diez años de golpe y tumbar el panel.
	if hasta.Sub(desde) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("el rango no puede pasar de un año")
	}
	return desde, hasta, nil
}

func fechaDe(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("falta la fecha")
	}
	f, err := time.Parse(iso, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("fecha inválida, se espera AAAA-MM-DD")
	}
	return f, nil
}

// zonaDeSucursal y jornadaDeSucursal traen la configuración de la sucursal; si
// no tiene fila propia, los valores de fábrica.
func (s *Servidor) zonaDeSucursal(r *http.Request, sucursalID string) string {
	if suc, err := s.q.SucursalPorID(r.Context(), sucursalID); err == nil && suc.Timezone != "" {
		return suc.Timezone
	}
	return "America/Havana"
}

func (s *Servidor) jornadaDeSucursal(r *http.Request, sucursalID string) (string, string) {
	if c, err := s.q.ConfigDeSucursal(r.Context(), sucursalID); err == nil {
		return c.JornadaInicio, c.JornadaFin
	}
	return "09:00", "16:00"
}

// fallo registra el error de verdad y devuelve uno genérico.
//
// El mensaje interno no sale nunca al cliente: una consulta fallida puede
// filtrar nombres de tablas y de columnas.
func (s *Servidor) fallo(w http.ResponseWriter, contexto string, err error) {
	s.log.Error("fallo en el panel", "donde", contexto, "error", err)
	responderError(w, http.StatusInternalServerError, "error interno")
}

func opcional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nuevoID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("sin fuente de aleatoriedad: %v", err))
	}
	return hex.EncodeToString(b)
}
