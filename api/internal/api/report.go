package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/calendar"
	"github.com/procovar/procovar-rutas/api/internal/report"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// A seller's report: every movement detailed between 9:00 and 16:00.
//
// The API returns the report as JSON and the front end lays it out and prints to
// PDF. Building the document deliberately does not live here: that way the same
// JSON serves the screen, the PDF and the spreadsheet, without three separate
// truths.
//
// It started as a "weekly report" and only took ?date=, from which it derived the
// whole working week. But a report is asked for in order to show it to someone,
// and there the normal thing is to narrow it down: three days, a single day, a
// fortnight. Now it takes ?from= and ?to= like the rest of the panel; without them
// it still returns the current week, which was the old behaviour.
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
	// The document's days are ALL of those in the range, not only the working ones:
	// someone asking for one particular Saturday wants to see that Saturday.
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

	// One day per section, bad ones included: a day with no file, no date or no
	// movement STILL gets its section with the reason written in. It is precisely
	// the one worth showing, so it cannot vanish from the document.
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

	s.auth.RecordAudit(r.Context(), "rutas.reporte", trabajadorID, c.AuthUserID)
	respond(w, http.StatusOK, doc)
}

// reportRange reads ?from= and ?to=. With neither, today's working week; with only
// ?date=, that date's week, which is how it was asked for before ranges existed.
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
