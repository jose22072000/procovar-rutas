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
	"github.com/procovar/procovar-rutas/api/internal/pedido"
	"github.com/procovar/procovar-rutas/api/internal/queue"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

type Server struct {
	cfg    *config.Config
	pool   *pgxpool.Pool
	q      *store.Queries
	auth   *auth.Client
	ingest *ingest.Service
	// pedidos may be nil: without PEDIDO_API_URL there is no crossing with orders
	// and the panel works exactly as it did before, minus that column.
	pedidos *pedido.Service
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
	svcPedidos *pedido.Service,
	colaRedis *queue.Queue,
	bus *events.Bus,
	log *slog.Logger,
) *Server {
	return &Server{
		cfg:     cfg,
		pool:    pool,
		q:       store.New(pool),
		auth:    cliAuth,
		ingest:  svcIngesta,
		pedidos: svcPedidos,
		queue:   colaRedis,
		bus:     bus,
		log:     log,
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
	r.Get("/api/ingest/folders", s.ingestFolders)
	r.Post("/api/ingest/folder-owner", s.ingestFolderOwner)
	r.Post("/api/ingest/known", s.ingestKnown)
	r.Get("/api/ingest/stats", s.ingestStats)

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
		r.Get("/sellers", s.sellers)

		// Cada vista, su llave. Se exige AQUÍ y no solo en la pantalla: una función
		// que desaparece del menú pero sigue contestando por su URL no está quitada.
		r.With(Exige(PermCalendario)).Get("/calendar", s.calendar)
		r.With(Exige(PermVisor)).Get("/day", s.day)
		r.With(Exige(PermVisor)).Get("/week", s.week)
		r.With(Exige(PermReporte)).Get("/report", s.report)

		// Los pedidos del día y su cruce con el recorrido.
		//
		// Se leen con la llave del calendario porque es AHÍ donde se enseñan: quien
		// puede ver el cumplimiento puede ver contra qué se está midiendo. Tocar
		// reutiliza las llaves que ya existen —emparejar es de la misma naturaleza
		// que un alias de dispositivo, y sincronizar que un barrido—, para no tener
		// que dar de alta llaves nuevas en Accesos.
		// Lo que hay que revisar: ficheros atascados, quién no sube y quién es quién
		// con PEDIDO. Con la llave del calendario porque es su detalle — quien puede
		// ver un hueco puede ver por qué está ahí.
		r.With(Exige(PermCalendario)).Get("/review", s.review)
		r.With(Exige(PermCalendario)).Get("/aliases", s.listAliases)
		r.With(Exige(PermCalendario)).Get("/pedidos/vendedores", s.vendedores)
		r.With(Exige(PermAlias)).Post("/pedidos/emparejar", s.emparejar)
		r.With(Exige(PermBarrido)).Post("/pedidos/sync", s.syncPedidos)

		r.Group(func(r chi.Router) {
			r.Use(Exige(PermBandeja))
			r.Get("/inbox", s.inbox)
			r.Post("/inbox/assign", s.assign)
		})

		r.Group(func(r chi.Router) {
			r.Use(Exige(PermAdministracion))
			r.Get("/sources", s.sources)
			r.Get("/scans", s.scans)
			r.Get("/queue", s.queueStats)
			r.Get("/ingest/status", s.adminIngestStats)

			// Ver Administración y TOCARLA son dos cosas: un gerente puede querer
			// mirar si las carpetas están al día sin poder darlas de baja.
			r.With(Exige(PermCarpeta)).Post("/sources", s.createSource)
			r.With(Exige(PermCarpeta)).Delete("/sources/{id}", s.deleteSource)
			r.With(Exige(PermAlias)).Post("/aliases", s.createAlias)
			r.With(Exige(PermAlias)).Delete("/aliases/{id}", s.deleteAlias)
			r.With(Exige(PermBarrido)).Post("/ingest/scan", s.scan)
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
