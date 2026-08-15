package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/calendar"
	"github.com/procovar/procovar-rutas/api/internal/report"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// El reporte semanal por vendedor: de lunes a viernes, con cada movimiento
// detallado entre las 9:00 y las 16:00.
//
// La API devuelve el reporte en JSON y el frontend lo maqueta e imprime a PDF.
// El armado del documento no vive aquí a propósito: así el mismo JSON sirve
// para la pantalla, para el PDF y para el Excel, sin tres verdades distintas.
// El reporte de un vendedor en el rango que se pida.
//
// Nació como "reporte semanal" y solo aceptaba ?date=, del que sacaba la week
// laboral entera. Pero el reporte se pide para enseñárselo a alguien, y ahí lo
// normal es acotar: tres días, un día suelto, la quincena. Ahora se le pasa
// ?from= y ?to= como al resto del panel; sin ellos sigue saliendo la week en
// curso, que era el comportamiento de antes.
func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	trabajadorID := r.URL.Query().Get("seller")
	if trabajadorID == "" {
		respondError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}

	desde, hasta, err := reportRange(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Los días del documento son TODOS los del rango, no solo los laborables: si
	// alguien pide un sábado concreto, es porque quiere ver ese sábado.
	diasDelRango := calendar.DaysBetween(desde, hasta)

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

	trab, err := s.q.SellerByID(r.Context(), trabajadorID)
	if err != nil {
		respondError(w, http.StatusNotFound, "ese vendedor no existe")
		return
	}

	dias, err := s.q.SellerWeek(r.Context(), store.SellerWeekParams{
		SellerID: trabajadorID, FromDate: desde, ToDate: hasta,
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.fail(w, "reporte", err)
		return
	}

	zona := s.branchZone(r, trab.BranchID)
	loc, err := time.LoadLocation(zona)
	if err != nil {
		loc = time.UTC
	}
	inicio, fin := s.branchWorkday(r, trab.BranchID)

	// Un día por sección, incluidos los malos: un día sin fichero, sin fecha o
	// sin movimiento LLEVA su sección con el motivo escrito. Es justo el que hay
	// que enseñar, así que no puede desaparecer del documento.
	porFecha := map[string]store.TrackDay{}
	for _, d := range dias {
		porFecha[d.Date.Format(iso)] = d
	}

	secciones := make([]report.Day, 0, len(diasDelRango))
	for _, f := range diasDelRango {
		clave := f.Format(iso)
		d, hay := porFecha[clave]
		if !hay {
			secciones = append(secciones, report.EmptyDay(clave))
			continue
		}

		puntos, err := s.q.DayPoints(r.Context(), store.DayPointsParams{
			TrackDayID: d.ID, Zone: zona, WorkdayStart: inicio, WorkdayEnd: fin,
		})
		if err != nil {
			s.fail(w, "puntos del reporte", err)
			return
		}
		paradas, err := s.q.DayStops(r.Context(), d.ID)
		if err != nil {
			s.fail(w, "paradas del reporte", err)
			return
		}

		secciones = append(secciones, report.BuildDay(d, puntos, paradas, loc))
	}

	doc := report.Build(report.Header{
		Seller:   trab.Name,
		SellerID: trab.ID,
		From:     desde.Format(iso),
		To:       hasta.Format(iso),
		Workday:  inicio + "–" + fin,
		Timezone: zona,
	}, secciones)

	s.auth.RegistrarAuditoria(r.Context(), "rutas.reporte", trabajadorID, c.AuthUserID)
	respond(w, http.StatusOK, doc)
}

// reportRange lee ?from= y ?to=. Sin nada, la week laboral de hoy; con solo
// ?date=, la week de esa fecha, que es como se pedía antes de aceptar rangos.
func reportRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	if q.Get("from") == "" && q.Get("to") == "" {
		base := time.Now()
		if d := q.Get("date"); d != "" {
			f, err := parseDate(d)
			if err != nil {
				return time.Time{}, time.Time{}, err
			}
			base = f
		}
		week := calendar.WorkWeek(base)
		return week[0], week[len(week)-1], nil
	}
	return queryRange(r)
}
