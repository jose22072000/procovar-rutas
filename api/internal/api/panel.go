package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/calendario"
)

// El calendario de cumplimiento: la cuadrícula de vendedores × días laborables
// que es la pantalla de entrada del panel.
func (s *Servidor) calendario(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	desde, hasta, err := rangoDeConsulta(r)
	if err != nil {
		responderError(w, http.StatusBadRequest, err.Error())
		return
	}

	// El alcance se calcula con la fecha CONSULTADA, no con hoy: si un supervisor
	// pide agosto en octubre, ve el equipo que tenía en agosto.
	filtro, err := c.Alcance(desde)
	if err != nil {
		responderError(w, http.StatusForbidden, err.Error())
		return
	}
	p := deFiltro(filtro)
	if p.Vacio {
		responder(w, http.StatusOK, map[string]any{"dias": []any{}, "resumen": []any{}})
		return
	}

	dias, err := s.q.Calendario(r.Context(), almacen.CalendarioParams{
		Desde: desde, Hasta: hasta,
		SucursalID: p.SucursalID, Trabajadores: p.Trabajadores, Excluir: p.Excluir,
	})
	if err != nil {
		s.fallo(w, "calendario", err)
		return
	}

	resumen, err := s.q.ResumenIncidencias(r.Context(), almacen.ResumenIncidenciasParams{
		Desde: desde, Hasta: hasta,
		SucursalID: p.SucursalID, Trabajadores: p.Trabajadores, Excluir: p.Excluir,
	})
	if err != nil {
		s.fallo(w, "resumen", err)
		return
	}

	responder(w, http.StatusOK, map[string]any{
		"desde":   desde.Format(iso),
		"hasta":   hasta.Format(iso),
		"dias":    dias,
		"resumen": resumen,
		// Los días laborables van al cliente para que la cuadrícula no tenga que
		// suponer cuáles son: se configuran por sucursal.
		"laborables": calendario.DiasLaborables(desde, hasta, nil),
	})
}

func (s *Servidor) vendedores(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	filtro, err := c.Alcance(time.Now())
	if err != nil {
		responderError(w, http.StatusForbidden, err.Error())
		return
	}
	p := deFiltro(filtro)
	if p.Vacio {
		responder(w, http.StatusOK, []any{})
		return
	}

	lista, err := s.q.TrabajadoresDelAlcance(r.Context(), almacen.TrabajadoresDelAlcanceParams{
		SucursalID: p.SucursalID, Trabajadores: p.Trabajadores, Excluir: p.Excluir,
	})
	if err != nil {
		s.fallo(w, "vendedores", err)
		return
	}
	responder(w, http.StatusOK, lista)
}

// El visor: el día de un vendedor con sus puntos y sus paradas.
func (s *Servidor) dia(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	trabajadorID := r.URL.Query().Get("vendedor")
	if trabajadorID == "" {
		responderError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}
	fecha, err := fechaDe(r.URL.Query().Get("fecha"))
	if err != nil {
		responderError(w, http.StatusBadRequest, err.Error())
		return
	}

	filtro, err := c.Alcance(fecha)
	if err != nil {
		responderError(w, http.StatusForbidden, err.Error())
		return
	}
	p := deFiltro(filtro)
	if p.Vacio {
		responderError(w, http.StatusForbidden, "sin acceso a ese vendedor")
		return
	}

	dia, err := s.q.DiaDeTrabajador(r.Context(), almacen.DiaDeTrabajadorParams{
		TrabajadorID: trabajadorID, Fecha: fecha,
		SucursalID: p.SucursalID, Trabajadores: p.Trabajadores, Excluir: p.Excluir,
	})
	if err != nil {
		// No distinguir "no existe" de "no puedes verlo" es deliberado: si el
		// mensaje fuera distinto, cualquiera podría averiguar qué días tiene un
		// vendedor de otro equipo probando fechas.
		responderError(w, http.StatusNotFound, "sin datos para ese día")
		return
	}

	zona := s.zonaDeSucursal(r, dia.SucursalID)
	inicio, fin := "00:00", "23:59"
	if r.URL.Query().Get("jornada") != "completa" {
		inicio, fin = s.jornadaDeSucursal(r, dia.SucursalID)
	}

	puntos, err := s.q.PuntosDeDia(r.Context(), almacen.PuntosDeDiaParams{
		TrackDayID: dia.ID, Zona: zona, JornadaInicio: inicio, JornadaFin: fin,
	})
	if err != nil {
		s.fallo(w, "puntos", err)
		return
	}

	paradas, err := s.q.ParadasDeDia(r.Context(), dia.ID)
	if err != nil {
		s.fallo(w, "paradas", err)
		return
	}

	responder(w, http.StatusOK, map[string]any{
		"dia":     dia,
		"puntos":  puntos,
		"paradas": paradas,
		"zona":    zona,
	})
}

func (s *Servidor) semana(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)

	trabajadorID := r.URL.Query().Get("vendedor")
	if trabajadorID == "" {
		responderError(w, http.StatusBadRequest, "falta el vendedor")
		return
	}
	fecha, err := fechaDe(r.URL.Query().Get("fecha"))
	if err != nil {
		responderError(w, http.StatusBadRequest, err.Error())
		return
	}

	// La semana del panel es LUNES A VIERNES, que es la jornada de la empresa.
	dias := calendario.SemanaLaboral(fecha)
	desde, hasta := dias[0], dias[len(dias)-1]

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

	filas, err := s.q.SemanaDeTrabajador(r.Context(), almacen.SemanaDeTrabajadorParams{
		TrabajadorID: trabajadorID, Desde: desde, Hasta: hasta,
		SucursalID: p.SucursalID, Trabajadores: p.Trabajadores, Excluir: p.Excluir,
	})
	if err != nil {
		s.fallo(w, "semana", err)
		return
	}

	responder(w, http.StatusOK, map[string]any{
		"desde": desde.Format(iso),
		"hasta": hasta.Format(iso),
		"dias":  filas,
	})
}
