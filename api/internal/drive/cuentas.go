package drive

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// Varias cuentas de Google, una por sucursal.
//
// El montaje real de Procovar: cada sucursal tiene su propia cuenta de Google,
// con el nombre de la sucursal, y las carpetas de rutas viven dentro. Es el
// mismo modelo que ya usa n8n, donde hay una credencial "Account padre" y otras
// por sucursal ("Granma", …).
//
// Por eso el sistema no habla con "un Drive" sino con un JUEGO de cuentas, y
// cada carpeta dice con cuál se lee. Si el día de mañana todas las carpetas se
// comparten en una sola cuenta, todas las fuentes apuntan a la misma clave y
// esto sigue funcionando sin tocar nada.

// Account es la credencial de una cuenta de Google.
type Account struct {
	// Key con la que la referencian las fuentes ("principal", "granma", …).
	Key string `json:"clave"`
	// Type: "oauth" (como n8n) o "service_account".
	Type string `json:"tipo"`

	// OAuth: lo mismo que n8n guarda en su credencial de Google Drive.
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`

	// Service account: el JSON de la clave.
	CredentialJSON string `json:"credencialJson,omitempty"`
}

// Set mantiene un cliente por cuenta y los crea a demanda.
type Set struct {
	cuentas  map[string]Account
	clientes map[string]Cliente
	mu       sync.Mutex
}

// LoadAccounts lee las credenciales de la configuración.
//
// `datos` es un JSON con la lista de cuentas. Se admite también una sola cuenta
// suelta, por comodidad cuando todavía no hay más que una.
func LoadAccounts(datos []byte) (*Set, error) {
	texto := strings.TrimSpace(string(datos))
	if texto == "" {
		return nil, fmt.Errorf("no hay ninguna cuenta de Google configurada")
	}

	var lista []Account
	if err := json.Unmarshal([]byte(texto), &lista); err != nil {
		var una Account
		if err2 := json.Unmarshal([]byte(texto), &una); err2 != nil {
			return nil, fmt.Errorf("las credenciales de Google no son un JSON válido: %w", err)
		}
		lista = []Account{una}
	}

	j := &Set{cuentas: map[string]Account{}, clientes: map[string]Cliente{}}
	for _, c := range lista {
		if c.Key == "" {
			c.Key = "principal"
		}
		if c.Type == "" {
			if c.RefreshToken != "" {
				c.Type = "oauth"
			} else {
				c.Type = "service_account"
			}
		}
		j.cuentas[c.Key] = c
	}
	if len(j.cuentas) == 0 {
		return nil, fmt.Errorf("la lista de cuentas de Google está vacía")
	}
	return j, nil
}

// CargarCuentasDeFichero es lo mismo, leyendo de disco.
func CargarCuentasDeFichero(ruta string) (*Set, error) {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", ruta, err)
	}
	return LoadAccounts(datos)
}

// Keys lista las cuentas configuradas.
func (j *Set) Keys() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.cuentas))
	for k := range j.cuentas {
		out = append(out, k)
	}
	return out
}

// Para devuelve el cliente de una cuenta.
//
// Si la clave no existe se cae a "principal" en vez de fallar: una carpeta mal
// etiquetada debe leerse con la cuenta por defecto, no quedarse sin barrer en
// silencio. Si tampoco hay principal, entonces sí es un error.
func (j *Set) Para(ctx context.Context, clave string) (Cliente, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if clave == "" {
		clave = "principal"
	}
	if _, hay := j.cuentas[clave]; !hay {
		if _, hayPrincipal := j.cuentas["principal"]; !hayPrincipal {
			return nil, fmt.Errorf("no hay credencial para la cuenta %q", clave)
		}
		clave = "principal"
	}

	if c, hay := j.clientes[clave]; hay {
		return c, nil
	}

	cuenta := j.cuentas[clave]
	cli, err := abrir(ctx, cuenta)
	if err != nil {
		return nil, err
	}
	j.clientes[clave] = cli
	return cli, nil
}

func abrir(ctx context.Context, c Account) (Cliente, error) {
	switch c.Type {
	case "service_account":
		return Nuevo(ctx, []byte(c.CredentialJSON))

	case "oauth":
		if c.ClientID == "" || c.ClientSecret == "" || c.RefreshToken == "" {
			return nil, fmt.Errorf("a la cuenta %q le faltan clientId, clientSecret o refreshToken", c.Key)
		}
		cfg := &oauth2.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			Endpoint:     google.Endpoint,
			// Solo lectura: este sistema nunca mueve ni borra nada del Drive de
			// los trabajadores, y conviene que ni siquiera pueda.
			Scopes: []string{drive.DriveReadonlyScope},
		}
		// El token de acceso se renueva solo con el de refresco.
		fuente := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: c.RefreshToken})
		svc, err := drive.NewService(ctx, option.WithTokenSource(fuente))
		if err != nil {
			return nil, fmt.Errorf("abriendo Drive de %q: %w", c.Key, err)
		}
		return &clienteGoogle{svc: svc}, nil

	default:
		return nil, fmt.Errorf("tipo de credencial desconocido en %q: %s", c.Key, c.Type)
	}
}
