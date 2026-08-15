package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/auth"
	"github.com/procovar/procovar-rutas/api/internal/cola"
	"github.com/procovar/procovar-rutas/api/internal/config"
	"github.com/procovar/procovar-rutas/api/internal/ingesta"
)

type Servidor struct {
	cfg     *config.Config
	pool    *pgxpool.Pool
	q       *almacen.Queries
	auth    *auth.Cliente
	ingesta *ingesta.Servicio
	// cola puede ser nil: sin Redis, el empuje de n8n se procesa en el acto.
	cola *cola.Cola
	log  *slog.Logger
}

func NuevoServidor(
	cfg *config.Config,
	pool *pgxpool.Pool,
	cliAuth *auth.Cliente,
	svcIngesta *ingesta.Servicio,
	colaRedis *cola.Cola,
	log *slog.Logger,
) *Servidor {
	return &Servidor{
		cfg:     cfg,
		pool:    pool,
		q:       almacen.New(pool),
		auth:    cliAuth,
		ingesta: svcIngesta,
		cola:    colaRedis,
		log:     log,
	}
}

func (s *Servidor) Rutas() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/salud", s.salud)

	// Puerta de servicio para n8n: sin sesión de usuario, con clave de máquina.
	r.Post("/api/ingesta/fichero", s.recibirFichero)

	// Flujo de login contra procovar-auth.
	r.Get("/api/auth/entrar", s.entrar)
	r.Get("/api/auth/callback", s.callback)
	r.Post("/api/auth/salir", s.salir)

	r.Route("/api", func(r chi.Router) {
		r.Use(s.ConSesion)

		r.Get("/yo", s.yo)
		r.Get("/calendario", s.calendario)
		r.Get("/vendedores", s.vendedores)
		r.Get("/dia", s.dia)
		r.Get("/semana", s.semana)
		r.Get("/reporte/semanal", s.reporteSemanal)

		r.Group(func(r chi.Router) {
			r.Use(SoloAdmin)
			r.Get("/bandeja", s.bandeja)
			r.Post("/bandeja/asignar", s.asignar)
			r.Get("/alias", s.listarAlias)
			r.Delete("/alias/{id}", s.borrarAlias)
			r.Get("/fuentes", s.fuentes)
			r.Post("/fuentes", s.crearFuente)
			r.Post("/ingesta/barrer", s.barrer)
			r.Get("/barridos", s.barridos)
			r.Get("/cola", s.estadoCola)
		})
	})

	return r
}

func (s *Servidor) salud(w http.ResponseWriter, r *http.Request) {
	estado := map[string]any{"ok": true}
	if err := s.pool.Ping(r.Context()); err != nil {
		estado["ok"] = false
		estado["base"] = err.Error()
		responder(w, http.StatusServiceUnavailable, estado)
		return
	}
	responder(w, http.StatusOK, estado)
}
