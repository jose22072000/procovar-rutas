package api

import (
	"net/http"
	"time"
)

// Flujo de login, según docs/CONSUMER-PATTERN.md de procovar-auth:
//
//	/api/auth/entrar    → pide un token de callback y redirige a procovar-auth
//	/api/auth/callback  → vuelve con ?code=…, se canjea por la sesión
//	/api/auth/salir     → borra la cookie

func (s *Servidor) entrar(w http.ResponseWriter, r *http.Request) {
	volverA := r.URL.Query().Get("volverA")
	if volverA == "" {
		volverA = s.cfg.AppURL
	}

	destino, err := s.auth.CrearTokenCallback(r.Context(), s.cfg.AppURL+"/api/auth/callback", volverA)
	if err != nil {
		s.log.Error("no se pudo arrancar el login", "error", err)
		responderError(w, http.StatusBadGateway, "procovar-auth no responde")
		return
	}
	http.Redirect(w, r, destino, http.StatusFound)
}

func (s *Servidor) callback(w http.ResponseWriter, r *http.Request) {
	codigo := r.URL.Query().Get("code")
	if codigo == "" {
		responderError(w, http.StatusBadRequest, "falta el código")
		return
	}

	res, err := s.auth.Canjear(r.Context(), codigo)
	if err != nil {
		s.log.Warn("canje fallido", "error", err)
		responderError(w, http.StatusUnauthorized, "no se pudo canjear el código")
		return
	}

	token, _ := res["sessionToken"].(string)
	if token == "" {
		responderError(w, http.StatusBadGateway, "procovar-auth no devolvió sesión")
		return
	}

	s.ponerCookie(w, token)

	destino, _ := res["returnTo"].(string)
	if destino == "" {
		destino = s.cfg.AppURL
	}
	http.Redirect(w, r, destino, http.StatusFound)
}

func (s *Servidor) salir(w http.ResponseWriter, r *http.Request) {
	// Se pone la cookie vacía con el MISMO dominio, no se borra: borrarla sin el
	// atributo de dominio deja viva la del dominio padre y la sesión "vuelve".
	http.SetCookie(w, s.cookie("", -1))
	responder(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Servidor) ponerCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, s.cookie(token, int((7*24*time.Hour).Seconds())))
}

func (s *Servidor) cookie(valor string, maxEdad int) *http.Cookie {
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

func (s *Servidor) yo(w http.ResponseWriter, r *http.Request) {
	c := DeContexto(r)
	responder(w, http.StatusOK, map[string]any{
		"user":     c.Nombre,
		"email":    c.Email,
		"role":     c.Rol,
		"branchId": c.SucursalID,
		"isAdmin":  c.Rol == "super_admin" || c.Rol == "admin",
	})
}
