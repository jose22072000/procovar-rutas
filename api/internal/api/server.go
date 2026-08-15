package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/auth"
	"github.com/procovar/procovar-rutas/api/internal/config"
	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/ingest"
	"github.com/procovar/procovar-rutas/api/internal/queue"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

type Server struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	q      *store.Queries
	auth   *auth.Client
	ingest *ingest.Service
	// queue may be nil: with no Redis, n8n's push is processed on the spot.
	queue *queue.Queue
	// bus may be nil for the same reason: with no Redis there are no live
	// notifications and the panel falls back to manual reloads. Not a reason to refuse to start.
	bus *events.Bus
	log *slog.Logger
}

func NewServer(
	cfg *config.Config,
	pool *pgxpool.Pool,
	cliAuth *auth.Client,
	svcIngesta *ingest.Service,
	colaRedis *queue.Queue,
	bus *events.Bus,
	log *slog.Logger,
) *Server {
	return &Server{
		cfg:    cfg,
		pool:   pool,
		q:      store.New(pool),
		auth:   cliAuth,
		ingest: svcIngesta,
		queue:  colaRedis,
		bus:    bus,
		log:    log,
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// The paths are in English even where the code inside is not: this is the
	// surface n8n, the front end and Traefik see, and languages do not mix there.
	r.Get("/health", s.health)

	// Service door for n8n: no user session, a machine key instead.
	r.Post("/api/ingest/file", s.receiveFile)

	// Login flow against procovar-auth.
	r.Get("/api/auth/login", s.login)
	r.Get("/api/auth/callback", s.callback)
	// GET, not POST: this is a navigation with a return trip, not a background call.
	r.Get("/api/auth/logout", s.logout)
	r.Get("/api/auth/logout/done", s.logoutDone)

	r.Route("/api", func(r chi.Router) {
		r.Use(s.WithSession)

		r.Get("/me", s.me)
		r.Get("/events", s.events)
		r.Get("/calendar", s.calendar)
		r.Get("/sellers", s.sellers)
		r.Get("/day", s.day)
		r.Get("/week", s.week)
		r.Get("/report", s.report)

		r.Group(func(r chi.Router) {
			r.Use(AdminOnly)
			r.Get("/inbox", s.inbox)
			r.Post("/inbox/assign", s.assign)
			r.Get("/aliases", s.listAliases)
			r.Delete("/aliases/{id}", s.deleteAlias)
			r.Get("/sources", s.sources)
			r.Post("/sources", s.createSource)
			r.Post("/ingest/scan", s.scan)
			r.Get("/scans", s.scans)
			r.Get("/queue", s.queueStats)
		})
	})

	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	estado := map[string]any{"ok": true}
	if err := s.pool.Ping(r.Context()); err != nil {
		estado["ok"] = false
		estado["base"] = err.Error()
		respond(w, http.StatusServiceUnavailable, estado)
		return
	}
	respond(w, http.StatusOK, estado)
}
