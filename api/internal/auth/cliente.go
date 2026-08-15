// Package auth habla con procovar-auth.
//
// Es el equivalente en Go de src/lib/sdk/qb-auth-client.ts, con el mismo
// esquema de firma HMAC: método, ruta, marca de tiempo, nonce y hash del cuerpo,
// unidos por saltos de línea y firmados con la clave del cliente. Si esto se
// desincroniza del TypeScript, la autenticación deja de funcionar entera — así
// que el orden de los campos es literalmente el de allí.
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

type Cliente struct {
	baseURL    string
	clientID   string
	clave      []byte
	keyVersion int
	http       *http.Client
}

func NuevoCliente(baseURL, clientID, claveHex string, keyVersion int) (*Cliente, error) {
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
	return &Cliente{
		baseURL:    strings.TrimRight(baseURL, "/"),
		clientID:   clientID,
		clave:      clave,
		keyVersion: keyVersion,
		http:       &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Organizacion es una sucursal, en el vocabulario de procovar-auth.
type Organizacion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type Membresia struct {
	ID           string       `json:"id"`
	Roles        []string     `json:"roles"`
	Organization Organizacion `json:"organization"`
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

// Sesion es la respuesta de /api/auth/verify-session.
type Sesion struct {
	Valid       bool          `json:"valid"`
	User        Usuario       `json:"user"`
	Session     SesionInterna `json:"session"`
	Memberships []Membresia   `json:"memberships"`
	Rbac        struct {
		Permissions []string `json:"permissions"`
		Roles       []string `json:"roles"`
	} `json:"rbac"`
}

// VerificarSesion cambia la cookie de sesión por la identidad de quien la trae.
func (c *Cliente) VerificarSesion(ctx context.Context, token string) (*Sesion, error) {
	var s Sesion
	if err := c.llamar(ctx, http.MethodPost, "/api/auth/verify-session",
		map[string]string{"sessionToken": token}, &s); err != nil {
		return nil, err
	}
	if !s.Valid {
		return nil, fmt.Errorf("sesión no válida")
	}
	return &s, nil
}

// CrearTokenCallback arranca el flujo de login y devuelve a dónde redirigir.
func (c *Cliente) CrearTokenCallback(ctx context.Context, callbackURL, returnTo string) (string, error) {
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

// Canjear cambia el código de vuelta del login por una sesión.
func (c *Cliente) Canjear(ctx context.Context, codigo string) (map[string]any, error) {
	var res map[string]any
	err := c.llamar(ctx, http.MethodPost, "/api/auth/exchange", map[string]string{"code": codigo}, &res)
	return res, err
}

// RegistrarAuditoria deja constancia en procovar-auth de una acción sensible
// (asignar un alias, cambiar una carpeta, exportar un reporte). Es best-effort:
// que la auditoría falle no puede tumbar la operación del usuario.
func (c *Cliente) RegistrarAuditoria(ctx context.Context, accion, recurso, usuarioID string) {
	_ = c.llamar(ctx, http.MethodPost, "/api/audit", map[string]string{
		"action":   accion,
		"resource": recurso,
		"userId":   usuarioID,
		"clientId": c.clientID,
	}, nil)
}

// firmar construye las cabeceras de servicio. El orden de los campos del texto
// a firmar tiene que ser IDÉNTICO al del SDK TypeScript.
func (c *Cliente) firmar(metodo, ruta string, cuerpo []byte) map[string]string {
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

func (c *Cliente) llamar(ctx context.Context, metodo, ruta string, cuerpo any, destino any) error {
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
	for k, v := range c.firmar(metodo, ruta, datos) {
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
