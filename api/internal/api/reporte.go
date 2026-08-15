package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/calendario"
	"github.com/procovar/procovar-rutas/api/internal/reporte"
)

// El reporte semanal por vendedor: de lunes a viernes, con cada movimiento
// detallado entre las 9:00 y las 16:00.
//
// La API devuelve el reporte en JSON y el frontend lo maqueta e imprime a PDF.
// El armado del documento no vive aquí a propósito: así el mismo JSON sirve
// para la pantalla, para el PDF y para el Excel, sin tres verdades distintas.
// El reporte de un vendedor en el rango que se pida.
//
// Nació como "reporte semanal" y solo aceptaba ?date=, del que sacaba la semana
// laboral entera. Pero el reporte se pide para enseñárselo a alguien, y ahí lo
// normal es acotar: tres días, un día suelto, la quincena. Ahora se le pasa
// ?from= y ?to= como al resto del panel; sin ellos sigue saliendo la semana en
// curso, que era el comportamiento de antes.
func (s *Servidor) reporteSemanal(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	trabajadorID := r.URL.Query().Get("seller")
	if trabajadorID == "" {
		responderError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}

	desde, hasta, err := rangoDeReporte(r)
	if err != nil {
		responderError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Los días del documento son TODOS los del rango, no solo los laborables: si
	// alguien pide un sábado concreto, es porque quiere ver ese sábado.
	diasDelRango := calendario.DiasEntre(desde, hasta)

	filtro, err := c.Alcance(desde)
	if err != nil {
		responderError(w, http.StatusForbidden, err.Error())
		return
	}
	p := deFiltro(filtro)
	if p.Vacio {
		responderError(w, http.StatusForbidden, "sin acceso a ese vendedor")
		return
	}

	trab, err := s.q.TrabajadorPorID(r.Context(), trabajadorID)
	if err != nil {
		responderError(w, http.StatusNotFound, "ese vendedor no existe")
		return
	}

	dias, err := s.q.SemanaDeTrabajador(r.Context(), almacen.SemanaDeTrabajadorParams{
		TrabajadorID: trabajadorID, Desde: desde, Hasta: hasta,
		SucursalID: p.SucursalID, Trabajadores: p.Trabajadores, Excluir: p.Excluir,
	})
	if err != nil {
		s.fallo(w, "reporte", err)
		return
	}

	zona := s.zonaDeSucursal(r, trab.SucursalID)
	loc, err := time.LoadLocation(zona)
	if err != nil {
		loc = time.UTC
	}
	inicio, fin := s.jornadaDeSucursal(r, trab.SucursalID)

	// Un día por sección, incluidos los malos: un día sin fichero, sin fecha o
	// sin movimiento LLEVA su sección con el motivo escrito. Es justo el que hay
	// que enseñar, así que no puede desaparecer del documento.
	porFecha := map[string]almacen.TrackDay{}
	for _, d := range dias {
		porFecha[d.Fecha.Format(iso)] = d
	}

	secciones := make([]reporte.Dia, 0, len(diasDelRango))
	for _, f := range diasDelRango {
		clave := f.Format(iso)
		d, hay := porFecha[clave]
		if !hay {
			secciones = append(secciones, reporte.DiaVacio(clave))
			continue
		}

		puntos, err := s.q.PuntosDeDia(r.Context(), almacen.PuntosDeDiaParams{
			TrackDayID: d.ID, Zona: zona, JornadaInicio: inicio, JornadaFin: fin,
		})
		if err != nil {
			s.fallo(w, "puntos del reporte", err)
			return
		}
		paradas, err := s.q.ParadasDeDia(r.Context(), d.ID)
		if err != nil {
			s.fallo(w, "paradas del reporte", err)
			return
		}

		secciones = append(secciones, reporte.ArmarDia(d, puntos, paradas, loc))
	}

	doc := reporte.Armar(reporte.Cabecera{
		Vendedor:   trab.Nombre,
		VendedorID: trab.ID,
		Desde:      desde.Format(iso),
		Hasta:      hasta.Format(iso),
		Jornada:    inicio + "–" + fin,
		Zona:       zona,
	}, secciones)

	s.auth.RegistrarAuditoria(r.Context(), "rutas.reporte", trabajadorID, c.AuthUserID)
	responder(w, http.StatusOK, doc)
}

// rangoDeReporte lee ?from= y ?to=. Sin nada, la semana laboral de hoy; con solo
// ?date=, la semana de esa fecha, que es como se pedía antes de aceptar rangos.
func rangoDeReporte(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	if q.Get("from") == "" && q.Get("to") == "" {
		base := time.Now()
		if d := q.Get("date"); d != "" {
			f, err := fechaDe(d)
			if err != nil {
				return time.Time{}, time.Time{}, err
			}
			base = f
		}
		semana := calendario.SemanaLaboral(base)
		return semana[0], semana[len(semana)-1], nil
	}
	return rangoDeConsulta(r)
}
