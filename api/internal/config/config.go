// Package config reads the configuration from the environment.
//
// Everything through environment variables, no config files: it is what Dokploy
// expects and it keeps a credential from being committed by accident.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Database.
	DatabaseURL string

	// Servidor.
	Puerto  string
	Entorno string // "dev" | "prod"

	// procovar-auth. See docs/CONSUMER-PATTERN.md in that repo.
	//
	// AuthURL is used server to server, and inside Dokploy that is the container's
	// internal name (`http://procovar-auth-xxxx:3500`), which does not leave the
	// Docker network.
	//
	// AuthPublicURL is the one the BROWSER has to reach. They are separate because
	// sending a person to the internal name is a dead end that only shows up when
	// somebody clicks: the login flow does not notice — the public URL comes back
	// in auth's own response — but signing out builds the address here.
	AuthURL        string
	AuthPublicURL  string
	AuthClientID   string
	AuthSigningKey string
	AppURL         string
	CookieDominio  string

	// Google Drive: there is ONE ACCOUNT PER BRANCH, each named after its branch.
	// GOOGLE_CUENTAS is the JSON list; each folder says which one reads it. They
	// are obtained with `go run ./cmd/authorize`.
	GoogleCredencialJSON string
	// Path to the credentials file, an alternative to pasting the whole JSON.
	GoogleCredencialPath string

	// Redis: the queue of files n8n pushes. The prefix keeps the keys apart from
	// PEDIDO's (procovar-pedido:*) in the same Redis.
	RedisURL     string
	PrefijoRedis string

	// ServiceKey authenticates n8n on POST /api/ingest/file. It is a machine key,
	// not a person's: a procovar-auth session will not do.
	ServiceKey string

	// PEDIDO: de dónde salen los pedidos del día con la geo de su cliente.
	//
	// PedidoURL es la dirección INTERNA del contenedor de PEDIDO
	// (`http://pedido-api-xxxx:8400`): los dos cuelgan de la misma red de Docker en
	// Dokploy, así que el tráfico no sale de la máquina — ni vuelta pública, ni TLS
	// que negociar, ni nada que publicar. Vacío = el cruce con pedidos queda
	// apagado y el panel funciona exactamente como antes.
	PedidoURL         string
	PedidoKey         string
	PedidoVentanaDias int
	IntervaloPedidos  time.Duration

	// Ingesta.
	IntervaloBarrido      time.Duration
	HoraRepasoNocturno    int // hora local, 0-23
	MaxFicherosPorBarrido int
}

func Cargar() (*Config, error) {
	c := &Config{
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		Puerto:                porDefecto("PUERTO", "3600"),
		Entorno:               porDefecto("ENTORNO", "dev"),
		AuthURL:               os.Getenv("QB_AUTH_URL"),
		AuthPublicURL:         porDefecto("QB_AUTH_PUBLIC_URL", "https://auth.procovar.cloud"),
		AuthClientID:          porDefecto("QB_AUTH_CLIENT_ID", "procovar-rutas"),
		AuthSigningKey:        os.Getenv("QB_AUTH_SIGNING_KEY"),
		AppURL:                os.Getenv("APP_URL"),
		CookieDominio:         os.Getenv("QB_SESSION_COOKIE_DOMAIN"),
		GoogleCredencialJSON:  os.Getenv("GOOGLE_CUENTAS"),
		GoogleCredencialPath:  os.Getenv("GOOGLE_CUENTAS_FILE"),
		RedisURL:              os.Getenv("REDIS_URL"),
		PrefijoRedis:          porDefecto("PREFIJO_REDIS", "procovar-rutas:"),
		ServiceKey:            os.Getenv("SERVICE_API_KEY"),
		PedidoURL:             os.Getenv("PEDIDO_API_URL"),
		PedidoKey:             porDefecto("PEDIDO_API_KEY", os.Getenv("SERVICE_API_KEY")),
		PedidoVentanaDias:     entero("PEDIDO_VENTANA_DIAS", 21),
		IntervaloPedidos:      duracion("INTERVALO_PEDIDOS", time.Hour),
		IntervaloBarrido:      duracion("INTERVALO_BARRIDO", 30*time.Minute),
		HoraRepasoNocturno:    entero("HORA_REPASO_NOCTURNO", 2),
		MaxFicherosPorBarrido: entero("MAX_FICHEROS_BARRIDO", 500),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("falta DATABASE_URL")
	}
	return c, nil
}

// Credentials returns the service account JSON, wherever it comes from.
func (c *Config) Credentials() ([]byte, error) {
	if strings.TrimSpace(c.GoogleCredencialJSON) != "" {
		return []byte(c.GoogleCredencialJSON), nil
	}
	if c.GoogleCredencialPath != "" {
		return os.ReadFile(c.GoogleCredencialPath)
	}
	return nil, fmt.Errorf(
		"faltan las cuentas de Google: pon GOOGLE_CUENTAS o GOOGLE_CUENTAS_FILE " +
			"(se generan con `go run ./cmd/authorize`)")
}

func porDefecto(clave, def string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return def
}

func entero(clave string, def int) int {
	if v := os.Getenv(clave); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func duracion(clave string, def time.Duration) time.Duration {
	if v := os.Getenv(clave); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
