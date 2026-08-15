package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/calendar"
)

const iso = "2006-01-02"

// queryRange lee ?from= y ?to=. Sin parámetros, la week laboral en curso,
// que es lo que se quiere ver al login.
//
// Los nombres de los parámetros van en inglés como el resto del API; los de dentro
// siguen en español, que es el idioma del código.
func queryRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	desdeStr, hastaStr := q.Get("from"), q.Get("to")

	if desdeStr == "" && hastaStr == "" {
		week := calendar.WorkWeek(time.Now())
		return week[0], week[len(week)-1], nil
	}

	desde, err := parseDate(desdeStr)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	hasta := desde
	if hastaStr != "" {
		hasta, err = parseDate(hastaStr)
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

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("falta la fecha")
	}
	f, err := time.Parse(iso, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("fecha inválida, se espera AAAA-MM-DD")
	}
	return f, nil
}

// branchZone y branchWorkday traen la configuración de la sucursal; si
// no tiene fila propia, los valores de fábrica.
func (s *Server) branchZone(r *http.Request, sucursalID string) string {
	if suc, err := s.q.BranchByID(r.Context(), sucursalID); err == nil && suc.Timezone != "" {
		return suc.Timezone
	}
	return "America/Havana"
}

func (s *Server) branchWorkday(r *http.Request, sucursalID string) (string, string) {
	if c, err := s.q.BranchConfig(r.Context(), sucursalID); err == nil {
		return c.WorkdayStart, c.WorkdayEnd
	}
	return "09:00", "16:00"
}

// fail registra el error de verdad y devuelve uno genérico.
//
// El mensaje interno no sale nunca al cliente: una consulta fallida puede
// filtrar nombres de tablas y de columnas.
func (s *Server) fail(w http.ResponseWriter, contexto string, err error) {
	s.log.Error("fail en el panel", "donde", contexto, "error", err)
	respondError(w, http.StatusInternalServerError, "error interno")
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("sin fuente de aleatoriedad: %v", err))
	}
	return hex.EncodeToString(b)
}
