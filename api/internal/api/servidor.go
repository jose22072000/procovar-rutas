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

	// Los caminos van en inglés aunque el código de dentro esté en español: es la
	// superficie que ven n8n, el front y Traefik, y ahí no se mezclan idiomas.
	r.Get("/health", s.salud)

	// Puerta de servicio para n8n: sin sesión de usuario, con clave de máquina.
	r.Post("/api/ingest/file", s.recibirFichero)

	// Flujo de login contra procovar-auth.
	r.Get("/api/auth/login", s.entrar)
	r.Get("/api/auth/callback", s.callback)
	r.Post("/api/auth/logout", s.salir)

	r.Route("/api", func(r chi.Router) {
		r.Use(s.ConSesion)

		r.Get("/me", s.yo)
		r.Get("/calendar", s.calendario)
		r.Get("/sellers", s.vendedores)
		r.Get("/day", s.dia)
		r.Get("/week", s.semana)
		r.Get("/report", s.reporteSemanal)

		r.Group(func(r chi.Router) {
			r.Use(SoloAdmin)
			r.Get("/inbox", s.bandeja)
			r.Post("/inbox/assign", s.asignar)
			r.Get("/aliases", s.listarAlias)
			r.Delete("/aliases/{id}", s.borrarAlias)
			r.Get("/sources", s.fuentes)
			r.Post("/sources", s.crearFuente)
			r.Post("/ingest/scan", s.barrer)
			r.Get("/scans", s.barridos)
			r.Get("/queue", s.estadoCola)
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
