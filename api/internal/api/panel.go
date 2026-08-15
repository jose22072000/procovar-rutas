package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/calendar"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// El calendar de cumplimiento: la cuadrícula de sellers × días laborables
// que es la pantalla de entrada del panel.
func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	desde, hasta, err := queryRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// El alcance se calcula con la fecha CONSULTADA, no con hoy: si un supervisor
	// pide agosto en octubre, ve el equipo que tenía en agosto.
	filtro, err := c.Scope(desde)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	p := fromFilter(filtro)
	if p.Empty {
		respond(w, http.StatusOK, map[string]any{"days": []any{}, "summary": []any{}})
		return
	}

	dias, err := s.q.Calendar(r.Context(), store.CalendarParams{
		FromDate: desde, ToDate: hasta,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.fail(w, "calendar", err)
		return
	}

	resumen, err := s.q.IncidentSummary(r.Context(), store.IncidentSummaryParams{
		FromDate: desde, ToDate: hasta,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.fail(w, "resumen", err)
		return
	}

	respond(w, http.StatusOK, map[string]any{
		"from":    desde.Format(iso),
		"to":      hasta.Format(iso),
		"days":    aSellerDays(dias),
		"summary": aSummaryRows(resumen),
		// Los días laborables van al cliente para que la cuadrícula no tenga que
		// suponer cuáles son: se configuran por sucursal.
		"workdays": calendar.Workdays(desde, hasta, nil),
	})
}

func (s *Server) sellers(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	filtro, err := c.Scope(time.Now())
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	p := fromFilter(filtro)
	if p.Empty {
		respond(w, http.StatusOK, []any{})
		return
	}

	lista, err := s.q.SellersInScope(r.Context(), store.SellersInScopeParams{
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.fail(w, "sellers", err)
		return
	}
	respond(w, http.StatusOK, aSellers(lista))
}

// El visor: el día de un vendedor con sus puntos y sus paradas.
func (s *Server) day(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	trabajadorID := r.URL.Query().Get("seller")
	if trabajadorID == "" {
		respondError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}
	fecha, err := parseDate(r.URL.Query().Get("date"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	filtro, err := c.Scope(fecha)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	p := fromFilter(filtro)
	if p.Empty {
		respondError(w, http.StatusForbidden, "sin acceso a ese vendedor")
		return
	}

	day, err := s.q.SellerDay(r.Context(), store.SellerDayParams{
		SellerID: trabajadorID, Date: fecha,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		// No distinguir "no existe" de "no puedes verlo" es deliberado: si el
		// mensaje fuera distinto, cualquiera podría averiguar qué días tiene un
		// vendedor de otro equipo probando fechas.
		respondError(w, http.StatusNotFound, "sin datos para ese día")
		return
	}

	zona := s.branchZone(r, day.BranchID)
	inicio, fin := "00:00", "23:59"
	if r.URL.Query().Get("workday") != "full" {
		inicio, fin = s.branchWorkday(r, day.BranchID)
	}

	puntos, err := s.q.DayPoints(r.Context(), store.DayPointsParams{
		TrackDayID: day.ID, Zone: zona, WorkdayStart: inicio, WorkdayEnd: fin,
	})
	if err != nil {
		s.fail(w, "puntos", err)
		return
	}

	paradas, err := s.q.DayStops(r.Context(), day.ID)
	if err != nil {
		s.fail(w, "paradas", err)
		return
	}

	// El nombre no viene en la fila del día, y el visor enseña "Ver el día de
	// <quién>": sin esto saldría el identificador. Si la consulta falla no se
	// tumba la página por un rótulo; se cae al identificador.
	nombreVendedor := trabajadorID
	if t, err := s.q.SellerByID(r.Context(), trabajadorID); err == nil {
		nombreVendedor = t.Name
	}

	respond(w, http.StatusOK, map[string]any{
		"day":      aDayDetail(day, nombreVendedor),
		"points":   aTrackPoints(puntos),
		"stops":    aStops(paradas),
		"timezone": zona,
	})
}

func (s *Server) week(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	trabajadorID := r.URL.Query().Get("seller")
	if trabajadorID == "" {
		respondError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}
	fecha, err := parseDate(r.URL.Query().Get("date"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// La week del panel es LUNES A VIERNES, que es la jornada de la empresa.
	dias := calendar.WorkWeek(fecha)
	desde, hasta := dias[0], dias[len(dias)-1]

	filtro, err := c.Scope(desde)
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	p := fromFilter(filtro)
	if p.Empty {
		respondError(w, http.StatusForbidden, "sin acceso a ese vendedor")
		return
	}

	nombreSemana := trabajadorID
	if t, err := s.q.SellerByID(r.Context(), trabajadorID); err == nil {
		nombreSemana = t.Name
	}

	filas, err := s.q.SellerWeek(r.Context(), store.SellerWeekParams{
		SellerID: trabajadorID, FromDate: desde, ToDate: hasta,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.fail(w, "week", err)
		return
	}

	respond(w, http.StatusOK, map[string]any{
		"from": desde.Format(iso),
		"to":   hasta.Format(iso),
		"days": aSellerDaysFromTrackDay(filas, nombreSemana),
	})
}
