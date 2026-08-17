package api

import (
	"net/http"
	"net/url"
	"time"
)

// Login flow, per procovar-auth's docs/CONSUMER-PATTERN.md:
//
//	/api/auth/login       → asks for a callback token and redirects to procovar-auth
//	/api/auth/callback    → comes back with ?code=…, exchanged for the session
//	/api/auth/logout      → sends to procovar-auth, which asks and closes
//	/api/auth/logout/done → on the way back, clears the cookie here

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	volverA := r.URL.Query().Get("returnTo")
	if volverA == "" {
		volverA = s.cfg.AppURL
	}

	destino, err := s.auth.CreateCallbackToken(r.Context(), s.cfg.AppURL+"/api/auth/callback", volverA)
	if err != nil {
		s.log.Error("no se pudo arrancar el login", "error", err)
		respondError(w, http.StatusBadGateway, "procovar-auth no responde")
		return
	}
	http.Redirect(w, r, destino, http.StatusFound)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	codigo := r.URL.Query().Get("code")
	if codigo == "" {
		respondError(w, http.StatusBadRequest, "falta el código")
		return
	}

	res, err := s.auth.Exchange(r.Context(), codigo)
	if err != nil {
		s.log.Warn("canje fallido", "error", err)
		respondError(w, http.StatusUnauthorized, "no se pudo canjear el código")
		return
	}

	token, _ := res["sessionToken"].(string)
	if token == "" {
		respondError(w, http.StatusBadGateway, "procovar-auth no devolvió sesión")
		return
	}

	s.setCookie(w, token)

	destino, _ := res["returnTo"].(string)
	if destino == "" {
		destino = s.cfg.AppURL
	}
	http.Redirect(w, r, destino, http.StatusFound)
}

// logout closes nothing on its own: it sends to procovar-auth.
//
// The session lives there. If this panel merely cleared its own cookie — which is
// what it used to do — "log out" would be a lie: the Accounts session would stay
// open and the login button would put you back in without asking a thing. That is
// why the "are you sure?" prompt also lives there and not here: one page, one
// wording, and the only place that really closes it.
//
// The cookie here is cleared on the way BACK (/api/auth/logout/done), not now. So
// if you say no at the prompt, nothing is left half-done: Accounts session intact
// and cookie here intact. Delivery, which clears before leaving, strands whoever
// cancels without a cookie and makes them log in again.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	// The PUBLIC address, not the internal one: this redirect is followed by the
	// browser, which knows nothing about the Docker network.
	v := url.Values{}
	v.Set("returnTo", s.cfg.AppURL+"/api/auth/logout/done")
	v.Set("cancelUrl", s.cfg.AppURL+"/")
	http.Redirect(w, r, s.cfg.AuthPublicURL+"/logout?"+v.Encode(), http.StatusFound)
}

// logoutDone is where the return from Accounts lands, that session already closed.
func (s *Server) logoutDone(w http.ResponseWriter, r *http.Request) {
	// The cookie is set empty with the SAME domain rather than deleted: deleting it
	// without the domain attribute leaves the parent-domain one alive and the session "comes back".
	http.SetCookie(w, s.cookie("", -1))
	http.Redirect(w, r, s.cfg.AppURL+"/", http.StatusFound)
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, s.cookie(token, int((7*24*time.Hour).Seconds())))
}

func (s *Server) cookie(valor string, maxEdad int) *http.Cookie {
	c := &http.Cookie{
		Name:     CookieSesion,
		Value:    valor,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Entorno != "dev",
		MaxAge:   maxEdad,
	}
	// With no explicit domain the cookie belongs to the host, which is right when
	// the applications do not share a root. With QB_SESSION_COOKIE_DOMAIN it is
	// shared across every *.procovar.cloud application.
	if s.cfg.CookieDominio != "" {
		c.Domain = s.cfg.CookieDominio
	}
	return c
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	// Qué llaves tiene, para que la pantalla esconda lo que no puede usar. La
	// pantalla no decide: pregunta. Y aunque no escondiera nada, la API contesta 403
	// igual, que es donde se sostiene de verdad.
	permisos := map[string]bool{}
	for _, k := range TodasLasLlaves {
		permisos[k] = c.Puede(k)
	}

	respond(w, http.StatusOK, map[string]any{
		"user":     c.Name,
		"email":    c.Email,
		"role":     c.Role,
		"branchId": c.BranchID,
		"isAdmin":  c.Puede(PermAdministracion),
		"permisos": permisos,
	})
}
