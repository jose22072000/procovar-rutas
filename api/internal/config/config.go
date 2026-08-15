// Package config lee la configuración del entorno.
//
// Todo por variables de entorno, nada de ficheros de configuración: es lo que
// espera Dokploy y evita que una credencial acabe versionada por accidente.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Base de datos.
	DatabaseURL string

	// Servidor.
	Puerto  string
	Entorno string // "dev" | "prod"

	// procovar-auth. Ver docs/CONSUMER-PATTERN.md en ese repo.
	AuthURL        string
	AuthClientID   string
	AuthSigningKey string
	AppURL         string
	CookieDominio  string

	// Google Drive: hay UNA CUENTA POR SUCURSAL, cada una con el nombre de su
	// sucursal. GOOGLE_CUENTAS es la lista en JSON; cada carpeta dice con cuál
	// se lee. Se consiguen con `go run ./cmd/autorizar`.
	GoogleCredencialJSON string
	// Ruta al fichero de credenciales, alternativa a pegar el JSON entero.
	GoogleCredencialPath string

	// Redis: la cola de ficheros que empuja n8n. El prefijo mantiene las claves
	// separadas de las de PEDIDO (procovar-pedido:*) en el mismo Redis.
	RedisURL     string
	PrefijoRedis string

	// ClaveServicio autentica a n8n en POST /api/ingesta/fichero. Es de máquina,
	// no de persona: no vale la sesión de procovar-auth.
	ClaveServicio string

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
		AuthClientID:          porDefecto("QB_AUTH_CLIENT_ID", "procovar-rutas"),
		AuthSigningKey:        os.Getenv("QB_AUTH_SIGNING_KEY"),
		AppURL:                os.Getenv("APP_URL"),
		CookieDominio:         os.Getenv("QB_SESSION_COOKIE_DOMAIN"),
		GoogleCredencialJSON:  os.Getenv("GOOGLE_CUENTAS"),
		GoogleCredencialPath:  os.Getenv("GOOGLE_CUENTAS_FILE"),
		RedisURL:              os.Getenv("REDIS_URL"),
		PrefijoRedis:          porDefecto("PREFIJO_REDIS", "procovar-rutas:"),
		ClaveServicio:         os.Getenv("SERVICE_API_KEY"),
		IntervaloBarrido:      duracion("INTERVALO_BARRIDO", 30*time.Minute),
		HoraRepasoNocturno:    entero("HORA_REPASO_NOCTURNO", 2),
		MaxFicherosPorBarrido: entero("MAX_FICHEROS_BARRIDO", 500),
	}

	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("falta DATABASE_URL")
	}
	return c, nil
}

// Credenciales devuelve el JSON de la service account, venga de donde venga.
func (c *Config) Credenciales() ([]byte, error) {
	if strings.TrimSpace(c.GoogleCredencialJSON) != "" {
		return []byte(c.GoogleCredencialJSON), nil
	}
	if c.GoogleCredencialPath != "" {
		return os.ReadFile(c.GoogleCredencialPath)
	}
	return nil, fmt.Errorf(
		"faltan las cuentas de Google: pon GOOGLE_CUENTAS o GOOGLE_CUENTAS_FILE " +
			"(se generan con `go run ./cmd/autorizar`)")
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
