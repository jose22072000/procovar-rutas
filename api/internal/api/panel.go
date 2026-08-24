package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/calendar"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// The compliance calendar: the grid of sellers × working days that is the panel's
// landing screen.
func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	desde, hasta, err := queryRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Scope is computed from the DATE BEING QUERIED, not from today: if a supervisor
	// asks in October for August, they see the team they had in August.
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

	// A partir de aquí, lo que convierte la cuadrícula en información.
	//
	// Antes esto devolvía kilómetros y una etiqueta, y las dos preguntas que se hacen
	// al abrirlo —¿subió?, y si no, ¿por qué?— había que ir a contestarlas a otra
	// pantalla. Ahora vienen en la misma respuesta: cuándo subió cada uno por última
	// vez, qué ficheros suyos se atascaron, y cuántos de sus pedidos del día pisó.
	//
	// Ninguna de las tres tumba el calendario si falla: son el porqué de lo que ya
	// se está pintando, no lo pintado. Un Redis caído o un PEDIDO que no contesta
	// dejan la cuadrícula con menos detalle, nunca en blanco.
	estados, err := store.UploadStates(r.Context(), s.pool, p.BranchID, p.Sellers, p.Exclude)
	if err != nil {
		s.log.Warn("sin estado de subida", "error", err)
	}

	atascados, err := s.q.StuckDays(r.Context(), store.StuckDaysParams{
		FromDate: desde, ToDate: hasta,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.log.Warn("sin ficheros atascados", "error", err)
	}

	visitas, err := s.q.VisitSummary(r.Context(), store.VisitSummaryParams{
		FromDate: desde, ToDate: hasta,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.log.Warn("sin cruce de pedidos", "error", err)
	}

	celdas := aSellerDays(dias)
	filas := aSummaryRows(resumen)
	conPedidos := s.pedidos != nil && s.pedidos.Configured()

	// Los pedidos, celda a celda. La clave es vendedor+día porque eso es lo que
	// identifica una celda de la cuadrícula.
	porCelda := make(map[string]store.VisitSummaryRow, len(visitas))
	porVendedor := map[string]struct{ orders, visited int32 }{}
	for _, v := range visitas {
		porCelda[v.SellerID+":"+v.Date.Format(iso)] = v
		acc := porVendedor[v.SellerID]
		acc.orders += v.Orders
		acc.visited += v.Visited
		porVendedor[v.SellerID] = acc
	}

	if conPedidos {
		for i := range celdas {
			if v, ok := porCelda[celdas[i].SellerID+":"+celdas[i].Date]; ok {
				pedidos, visitados := v.Orders, v.Visited
				celdas[i].Orders, celdas[i].Visited = &pedidos, &visitados
			}
		}
	}

	porTrabajador := make(map[string]store.UploadState, len(estados))
	for _, e := range estados {
		porTrabajador[e.SellerID] = e
	}
	hoy := time.Now().Truncate(24 * time.Hour)
	for i := range filas {
		if e, ok := porTrabajador[filas[i].SellerID]; ok {
			filas[i].StuckFiles = e.StuckFiles
			filas[i].Linked = e.Linked
			if e.LastUpload != nil {
				ultima := e.LastUpload.Format(iso)
				filas[i].LastUpload = &ultima
				filas[i].DaysSilent = int(hoy.Sub(*e.LastUpload).Hours() / 24)
			} else {
				// -1 y no 0: por ahí no ha entrado NUNCA nada, que no es lo mismo
				// que haber subido hoy.
				filas[i].DaysSilent = -1
			}
		}
		if acc, ok := porVendedor[filas[i].SellerID]; ok {
			filas[i].Orders, filas[i].Visited = acc.orders, acc.visited
		}
	}

	respond(w, http.StatusOK, map[string]any{
		"from":    desde.Format(iso),
		"to":      hasta.Format(iso),
		"days":    celdas,
		"summary": filas,
		"stuck":   aStuckDays(atascados),
		// Si esto es falso, la pantalla NO pinta la columna de pedidos: mejor no
		// decir nada que decir cero cuando lo que pasa es que no hay con qué medir.
		"withOrders": conPedidos,
		// The working days go to the client so the grid does not have to guess which
		// ones they are: they are configured per branch.
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

// The viewer: a seller's day with its points and its stops.
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
		// Not telling "does not exist" apart from "you may not see it" is
		// deliberate: if the
		// message differed, anyone could work out which days a seller from another
		// team has by trying dates.
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

	// The name is not in the day row, and the viewer shows "day of <whom>": without
	// this it would print the identifier. If the lookup fails the page is not
	// brought down over a label; it falls back to the identifier.
	nombreVendedor := trabajadorID
	if t, err := s.q.SellerByID(r.Context(), trabajadorID); err == nil {
		nombreVendedor = t.Name
	}

	// Los clientes del día: la capa que se enciende y se apaga sobre el mapa. Si el
	// cruce no está configurado la lista viene vacía y el visor no ofrece la capa,
	// en vez de ofrecer una capa que no enciende nada.
	visitas, err := s.q.DayVisits(r.Context(), day.ID)
	if err != nil {
		s.log.Warn("sin visitas del día", "dia", day.ID, "error", err)
	}

	respond(w, http.StatusOK, map[string]any{
		"day":      aDayDetail(day, nombreVendedor),
		"points":   aTrackPoints(puntos),
		"stops":    aStops(paradas),
		"visits":   aVisits(visitas),
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

	// The panel's week is MONDAY TO FRIDAY, the company's working week.
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
	sucursalSemana := ""
	if t, err := s.q.SellerByID(r.Context(), trabajadorID); err == nil {
		nombreSemana = t.Name
		if suc, err := s.q.BranchByID(r.Context(), t.BranchID); err == nil {
			sucursalSemana = suc.Name
		}
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
		"days": aSellerDaysFromTrackDay(filas, nombreSemana, sucursalSemana),
	})
}
