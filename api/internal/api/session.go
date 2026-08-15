package api

import (
	"net/http"
	"net/url"
	"time"
)

// Flujo de login, según docs/CONSUMER-PATTERN.md de procovar-auth:
//
//	/api/auth/login       → pide un token de callback y redirige a procovar-auth
//	/api/auth/callback    → vuelve con ?code=…, se canjea por la sesión
//	/api/auth/logout      → manda a procovar-auth, que pregunta y cierra
//	/api/auth/logout/done → al volver de allí, borra la cookie de aquí

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	volverA := r.URL.Query().Get("returnTo")
	if volverA == "" {
		volverA = s.cfg.AppURL
	}

	destino, err := s.auth.CrearTokenCallback(r.Context(), s.cfg.AppURL+"/api/auth/callback", volverA)
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

	res, err := s.auth.Canjear(r.Context(), codigo)
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

// logout NO cierra nada por su cuenta: manda a procovar-auth.
//
// La sesión vive allí. Si este panel se limitara a borrar su cookie —que es lo
// que hacía—, "cerrar sesión" sería mentira: la sesión de Accesos seguiría
// abierta y el botón de login devolvería adentro sin preguntar nada. Por eso el
// cartel de "¿seguro?" también está allí y no aquí: una sola página, un solo
// texto, y el único sitio que la cierra de verdad.
//
// La cookie de aquí se borra al VOLVER (/api/auth/logout/done), no ahora. Así, si
// en el cartel se dice que no, no queda a medias: sesión de Accesos intacta y
// cookie de aquí intacta. Delivery, que borra antes de ir, deja al que cancela
// sin cookie y le toca volver a login.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	v := url.Values{}
	v.Set("returnTo", s.cfg.AppURL+"/api/auth/logout/done")
	v.Set("cancelUrl", s.cfg.AppURL+"/")
	http.Redirect(w, r, s.cfg.AuthURL+"/logout?"+v.Encode(), http.StatusFound)
}

// logoutDone es donde aterriza la vuelta de Accesos, ya cerrada la sesión de allí.
func (s *Server) logoutDone(w http.ResponseWriter, r *http.Request) {
	// Se pone la cookie vacía con el MISMO dominio, no se borra: borrarla sin el
	// atributo de dominio deja viva la del dominio padre y la sesión "vuelve".
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
	// Sin dominio explícito la cookie es del host, que es lo correcto cuando las
	// aplicaciones no comparten raíz. Con QB_SESSION_COOKIE_DOMAIN se comparte
	// entre todas las de *.procovar.cloud.
	if s.cfg.CookieDominio != "" {
		c.Domain = s.cfg.CookieDominio
	}
	return c
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)
	respond(w, http.StatusOK, map[string]any{
		"user":     c.Name,
		"email":    c.Email,
		"role":     c.Role,
		"branchId": c.BranchID,
		"isAdmin":  c.Role == "super_admin" || c.Role == "admin",
	})
}
