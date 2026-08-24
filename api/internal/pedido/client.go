// Package pedido reads PEDIDO's orders and crosses them with the route.
//
// It is the answer to the only question the kilometres never answered: the seller
// did 30 km, but did they go past their clients? Thirty kilometres are just as
// easy to rack up driving in circles.
//
// PEDIDO is the OWNER of clients and orders, and that is not up for discussion
// here: this package only READS. It never writes back — not a price, not a status,
// nothing. If a row is wrong, it gets fixed over there and comes right on the next
// sync.
//
// It talks to PEDIDO's integration API over HTTP with the shared machine key
// (`x-api-key`), the same door delivery already uses. Server to server and INSIDE
// the server: in Dokploy both containers hang off the same Docker network, so
// PEDIDO_API_URL is its internal name and the traffic never leaves the machine —
// no public round trip, no TLS to negotiate, and nothing to publish.
package pedido

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to PEDIDO's integration API.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient returns nil when there is no configuration: the whole feature is
// optional and the panel has to work without it, showing that there are no orders
// rather than refusing to start.
func NewClient(baseURL, apiKey string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		// A generous timeout: a branch with months of history answers with
		// thousands of orders, and this runs in the background, not while
		// somebody waits in front of a screen.
		http: &http.Client{Timeout: 2 * time.Minute},
	}
}

// Vendor is the seller as PEDIDO writes them: their own master file, not the name
// of a Drive folder. Pairing the two is match.go's job.
type Vendor struct {
	ID     string `json:"id"`
	Code   string `json:"codigo"`
	Name   string `json:"nombre"`
	Active bool   `json:"activo"`
}

// Client_ is PEDIDO's client. Only geolocated ones arrive: without lat/lng there
// is nothing to measure and PEDIDO already filters them out at source.
type Client_ struct {
	ID           string   `json:"id"`
	Code         *string  `json:"codigo"`
	Name         string   `json:"nombre"`
	Zone         *string  `json:"zona"`
	Address      *string  `json:"direccion"`
	Municipality *string  `json:"municipio"`
	Lat          *float64 `json:"latitud"`
	Lon          *float64 `json:"longitud"`
}

// Order is one order of one day.
type Order struct {
	ID            string   `json:"id"`
	Folio         *string  `json:"folio"`
	BranchCode    *string  `json:"sucursalCodigo"`
	BranchName    *string  `json:"sucursalNombre"`
	Date          string   `json:"fecha"`
	Status        *string  `json:"estado"`
	NeedsDelivery bool     `json:"requiereDomicilio"`
	Vendor        *Vendor  `json:"vendedor"`
	Client        *Client_ `json:"cliente"`
	// UpdatedAt es el reloj de PEDIDO, y es lo que hace posible pedir sólo lo que se
	// movió: se guarda y en la siguiente pasada se manda como `since`.
	UpdatedAt *time.Time `json:"updatedAt"`
}

type ordersResponse struct {
	Count  int     `json:"count"`
	Orders []Order `json:"orders"`
}

// Orders brings the orders of a range of days, with their client and their
// vendor. PEDIDO scopes them to ITS OWN branch, so nothing else has to be asked:
// each installation hands over what is its own.
//
// `since` no es nulo = INCREMENTAL: sólo lo que se movió en PEDIDO desde ese
// instante. Es lo que evita bajarse tres semanas enteras cada hora para volver a
// escribir, idénticas, las mismas ocho mil filas. Suele no traer nada o cuatro
// filas.
func (c *Client) Orders(ctx context.Context, from, to time.Time, since *time.Time) ([]Order, error) {
	q := url.Values{}
	q.Set("desde", from.Format("2006-01-02"))
	q.Set("hasta", to.Format("2006-01-02"))
	if since != nil {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}

	var r ordersResponse
	if err := c.get(ctx, "/integration/orders", q, &r); err != nil {
		return nil, err
	}
	return r.Orders, nil
}

type clientsResponse struct {
	Count      int       `json:"count"`
	Clients    []Client_ `json:"clients"`
	NextCursor *string   `json:"nextCursor"`
}

// Clients brings the whole geolocated client book, paginated. It is the map layer
// that shows where ALL of a branch's clients are, not only those who ordered
// today.
func (c *Client) Clients(ctx context.Context) ([]Client_, error) {
	const porPagina = 1000

	out := []Client_{}
	cursor := ""
	// A hard stop: PEDIDO decides when the cursor ends, and a bug over there must
	// not turn into an infinite loop over here.
	for vuelta := 0; vuelta < 100; vuelta++ {
		q := url.Values{}
		q.Set("limit", fmt.Sprint(porPagina))
		if cursor != "" {
			q.Set("cursor", cursor)
		}

		var r clientsResponse
		if err := c.get(ctx, "/integration/clients", q, &r); err != nil {
			return nil, err
		}
		out = append(out, r.Clients...)

		if r.NextCursor == nil || *r.NextCursor == "" {
			return out, nil
		}
		cursor = *r.NextCursor
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, ruta string, q url.Values, destino any) error {
	dir := c.baseURL + ruta
	if len(q) > 0 {
		dir += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dir, nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("PEDIDO no contesta (%s): %w", ruta, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// The body is included because PEDIDO explains itself: a 403 there says
		// "this installation is branch X, not Y", and that is exactly what has to
		// be read in the log instead of a bare number.
		cuerpo, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("PEDIDO contestó %d en %s: %s", res.StatusCode, ruta, strings.TrimSpace(string(cuerpo)))
	}

	return json.NewDecoder(res.Body).Decode(destino)
}
