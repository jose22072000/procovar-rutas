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

// Several Google accounts, one per branch.
//
// Procovar's real setup: each branch has its own Google account, and the routes
// named after the branch, and the route folders live inside. It is the same model
// n8n already uses, where there is a "parent account" credential and others
// per branch ("Granma", …).
//
// That is why the system does not talk to "a Drive" but to a SET of accounts, and
// each folder says which one reads it. If one day every folder is shared into a
// single account, all sources point at the same key and this keeps working without
// a change.

// Account is one Google account's credential.
type Account struct {
	// Key the sources refer to it by ("principal", "granma", …).
	Key string `json:"clave"`
	// Type: "oauth" (como n8n) o "service_account".
	Type string `json:"tipo"`

	// OAuth: the same thing n8n stores in its Google Drive credential.
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`

	// Service account: the key's JSON.
	CredentialJSON string `json:"credencialJson,omitempty"`
}

// Set keeps one client per account and builds them on demand.
type Set struct {
	accounts map[string]Account
	clientes map[string]Client
	mu       sync.Mutex
}

// LoadAccounts reads the credentials from configuration.
//
// `data` is a JSON list of accounts. A single loose account is also accepted, for
// convenience while there is still only one.
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

	j := &Set{accounts: map[string]Account{}, clientes: map[string]Client{}}
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
		j.accounts[c.Key] = c
	}
	if len(j.accounts) == 0 {
		return nil, fmt.Errorf("la lista de accounts de Google está vacía")
	}
	return j, nil
}

// LoadAccountsFromFile is the same thing, reading from disk.
func LoadAccountsFromFile(ruta string) (*Set, error) {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", ruta, err)
	}
	return LoadAccounts(datos)
}

// Keys lists the configured accounts.
func (j *Set) Keys() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, 0, len(j.accounts))
	for k := range j.accounts {
		out = append(out, k)
	}
	return out
}

// For returns an account's client.
//
// When the key does not exist it falls back to "principal" instead of failing: a
// mislabelled folder should be read with the default account, not go unscanned in
// silence. If there is no principal either, then it really is an error.
func (j *Set) For(ctx context.Context, clave string) (Client, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if clave == "" {
		clave = "principal"
	}
	if _, hay := j.accounts[clave]; !hay {
		if _, hayPrincipal := j.accounts["principal"]; !hayPrincipal {
			return nil, fmt.Errorf("no hay credencial para la cuenta %q", clave)
		}
		clave = "principal"
	}

	if c, hay := j.clientes[clave]; hay {
		return c, nil
	}

	cuenta := j.accounts[clave]
	cli, err := abrir(ctx, cuenta)
	if err != nil {
		return nil, err
	}
	j.clientes[clave] = cli
	return cli, nil
}

func abrir(ctx context.Context, c Account) (Client, error) {
	switch c.Type {
	case "service_account":
		return New(ctx, []byte(c.CredentialJSON))

	case "oauth":
		if c.ClientID == "" || c.ClientSecret == "" || c.RefreshToken == "" {
			return nil, fmt.Errorf("a la cuenta %q le faltan clientId, clientSecret o refreshToken", c.Key)
		}
		cfg := &oauth2.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			Endpoint:     google.Endpoint,
			// Read-only: this system never moves or deletes anything in the sellers'
			// Drive, and it is better that it cannot even try.
			Scopes: []string{drive.DriveReadonlyScope},
		}
		// The access token refreshes itself using the refresh token.
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
