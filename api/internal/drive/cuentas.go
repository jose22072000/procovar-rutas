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
// mismo modelo que ya usa n8n, donde hay una credencial "Cuenta padre" y otras
// por sucursal ("Granma", …).
//
// Por eso el sistema no habla con "un Drive" sino con un JUEGO de cuentas, y
// cada carpeta dice con cuál se lee. Si el día de mañana todas las carpetas se
// comparten en una sola cuenta, todas las fuentes apuntan a la misma clave y
// esto sigue funcionando sin tocar nada.

// Cuenta es la credencial de una cuenta de Google.
type Cuenta struct {
	// Clave con la que la referencian las fuentes ("principal", "granma", …).
	Clave string `json:"clave"`
	// Tipo: "oauth" (como n8n) o "service_account".
	Tipo string `json:"tipo"`

	// OAuth: lo mismo que n8n guarda en su credencial de Google Drive.
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`

	// Service account: el JSON de la clave.
	CredencialJSON string `json:"credencialJson,omitempty"`
}

// Juego mantiene un cliente por cuenta y los crea a demanda.
type Juego struct {
	cuentas  map[string]Cuenta
	clientes map[string]Cliente
	mu       sync.Mutex
}

// CargarCuentas lee las credenciales de la configuración.
//
// `datos` es un JSON con la lista de cuentas. Se admite también una sola cuenta
// suelta, por comodidad cuando todavía no hay más que una.
func CargarCuentas(datos []byte) (*Juego, error) {
	texto := strings.TrimSpace(string(datos))
	if texto == "" {
		return nil, fmt.Errorf("no hay ninguna cuenta de Google configurada")
	}

	var lista []Cuenta
	if err := json.Unmarshal([]byte(texto), &lista); err != nil {
		var una Cuenta
		if err2 := json.Unmarshal([]byte(texto), &una); err2 != nil {
			return nil, fmt.Errorf("las credenciales de Google no son un JSON válido: %w", err)
		}
		lista = []Cuenta{una}
	}

	j := &Juego{cuentas: map[string]Cuenta{}, clientes: map[string]Cliente{}}
	for _, c := range lista {
		if c.Clave == "" {
			c.Clave = "principal"
		}
		if c.Tipo == "" {
			if c.RefreshToken != "" {
				c.Tipo = "oauth"
			} else {
				c.Tipo = "service_account"
			}
		}
		j.cuentas[c.Clave] = c
	}
	if len(j.cuentas) == 0 {
		return nil, fmt.Errorf("la lista de cuentas de Google está vacía")
	}
	return j, nil
}

// CargarCuentasDeFichero es lo mismo, leyendo de disco.
func CargarCuentasDeFichero(ruta string) (*Juego, error) {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", ruta, err)
	}
	return CargarCuentas(datos)
}

// Claves lista las cuentas configuradas.
func (j *Juego) Claves() []string {
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
func (j *Juego) Para(ctx context.Context, clave string) (Cliente, error) {
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

func abrir(ctx context.Context, c Cuenta) (Cliente, error) {
	switch c.Tipo {
	case "service_account":
		return Nuevo(ctx, []byte(c.CredencialJSON))

	case "oauth":
		if c.ClientID == "" || c.ClientSecret == "" || c.RefreshToken == "" {
			return nil, fmt.Errorf("a la cuenta %q le faltan clientId, clientSecret o refreshToken", c.Clave)
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
			return nil, fmt.Errorf("abriendo Drive de %q: %w", c.Clave, err)
		}
		return &clienteGoogle{svc: svc}, nil

	default:
		return nil, fmt.Errorf("tipo de credencial desconocido en %q: %s", c.Clave, c.Tipo)
	}
}
