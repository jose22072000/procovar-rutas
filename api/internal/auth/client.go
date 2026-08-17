// Package auth talks to procovar-auth.
//
// It is the Go counterpart of src/lib/sdk/qb-auth-client.ts, with the same HMAC
// signing scheme: method, path, timestamp, nonce and body hash, joined by
// newlines and signed with the client key. If this drifts from the TypeScript,
// authentication stops working entirely — so the field order is literally the one
// used over there.
package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	clientID   string
	clave      []byte
	keyVersion int
	http       *http.Client
}

func NuevoCliente(baseURL, clientID, claveHex string, keyVersion int) (*Client, error) {
	if baseURL == "" || clientID == "" {
		return nil, fmt.Errorf("faltan QB_AUTH_URL o QB_AUTH_CLIENT_ID")
	}
	if len(claveHex) != 64 {
		return nil, fmt.Errorf("QB_AUTH_SIGNING_KEY debe ser hex de 64 caracteres")
	}
	clave, err := hex.DecodeString(claveHex)
	if err != nil {
		return nil, fmt.Errorf("QB_AUTH_SIGNING_KEY no es hex válido: %w", err)
	}
	if keyVersion == 0 {
		keyVersion = 1
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		clientID:   clientID,
		clave:      clave,
		keyVersion: keyVersion,
		http:       &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Organization is a branch, in procovar-auth's vocabulary.
type Organization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Membresia struct {
	ID           string       `json:"id"`
	Roles        []string     `json:"roles"`
	Organization Organization `json:"organization"`
}

type Usuario struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	IsSystemAdmin bool   `json:"isSystemAdmin"`
}

type SesionInterna struct {
	ID                   string `json:"id"`
	ActiveOrganizationID string `json:"activeOrganizationId"`
}

// Session is the response from /api/auth/verify-session.
type Session struct {
	Valid       bool          `json:"valid"`
	User        Usuario       `json:"user"`
	Session     SesionInterna `json:"session"`
	Memberships []Membresia   `json:"memberships"`
	// Rol es el de la PERSONA, tal como lo escribe procovar-auth ("SUPERVISOR").
	Rol  string `json:"role"`
	Rbac struct {
		// Wildcard: el Super Admin, que puede en todas partes sin pertenecer a
		// ninguna sucursal.
		Wildcard bool `json:"wildcard"`
		// Las dos llegan con el mismo contenido; `global` es como lo llama
		// procovar-auth por dentro y `permissions` como lo publica.
		Global      []string `json:"global"`
		Permissions []string `json:"permissions"`
		Roles       []string `json:"roles"`
	} `json:"rbac"`
}

// VerifySession exchanges the session cookie for the identity of whoever brings it.
func (c *Client) VerifySession(ctx context.Context, token string) (*Session, error) {
	var s Session
	if err := c.llamar(ctx, http.MethodPost, "/api/auth/verify-session",
		map[string]string{"sessionToken": token}, &s); err != nil {
		return nil, err
	}
	if !s.Valid {
		return nil, fmt.Errorf("sesión no válida")
	}
	return &s, nil
}

// CreateCallbackToken starts the login flow and returns where to redirect.
func (c *Client) CreateCallbackToken(ctx context.Context, callbackURL, returnTo string) (string, error) {
	var res struct {
		RedirectURL string `json:"redirectUrl"`
	}
	err := c.llamar(ctx, http.MethodPost, "/api/auth/callback-token", map[string]string{
		"clientId":    c.clientID,
		"callbackUrl": callbackURL,
		"returnTo":    returnTo,
	}, &res)
	return res.RedirectURL, err
}

// Exchange swaps the code returned by the login for a session.
func (c *Client) Exchange(ctx context.Context, codigo string) (map[string]any, error) {
	var res map[string]any
	err := c.llamar(ctx, http.MethodPost, "/api/auth/exchange", map[string]string{"code": codigo}, &res)
	return res, err
}

// RecordAudit leaves a trace in procovar-auth of a sensitive action (assigning an
// alias, changing a folder, exporting a report). It is best-effort: a failed audit
// entry cannot bring down the user's operation.
func (c *Client) RecordAudit(ctx context.Context, accion, recurso, usuarioID string) {
	_ = c.llamar(ctx, http.MethodPost, "/api/audit", map[string]string{
		"action":   accion,
		"resource": recurso,
		"userId":   usuarioID,
		"clientId": c.clientID,
	}, nil)
}

// sign builds the service headers. The order of the fields in the string to sign
// has to be IDENTICAL to the TypeScript SDK's.
func (c *Client) sign(metodo, ruta string, cuerpo []byte) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		panic(fmt.Sprintf("sin fuente de aleatoriedad para el nonce: %v", err))
	}
	nonce := hex.EncodeToString(nonceBytes)

	hashCuerpo := sha256.Sum256(cuerpo)
	texto := strings.Join([]string{
		strings.ToUpper(metodo), ruta, ts, nonce, hex.EncodeToString(hashCuerpo[:]),
	}, "\n")

	mac := hmac.New(sha256.New, c.clave)
	mac.Write([]byte(texto))

	cab := map[string]string{
		"x-client-id":  c.clientID,
		"x-timestamp":  ts,
		"x-nonce":      nonce,
		"x-signature":  hex.EncodeToString(mac.Sum(nil)),
		"content-type": "application/json",
	}
	if c.keyVersion != 1 {
		cab["x-key-version"] = strconv.Itoa(c.keyVersion)
	}
	return cab
}

func (c *Client) llamar(ctx context.Context, metodo, ruta string, cuerpo any, destino any) error {
	var datos []byte
	if cuerpo != nil {
		var err error
		datos, err = json.Marshal(cuerpo)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, metodo, c.baseURL+ruta, bytes.NewReader(datos))
	if err != nil {
		return err
	}
	for k, v := range c.sign(metodo, ruta, datos) {
		req.Header.Set(k, v)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("llamando a procovar-auth: %w", err)
	}
	defer res.Body.Close()

	texto, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return fmt.Errorf("procovar-auth devolvió %d: %s", res.StatusCode, string(texto))
	}
	if destino == nil || len(texto) == 0 {
		return nil
	}
	return json.Unmarshal(texto, destino)
}
