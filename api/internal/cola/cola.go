// Package cola es la cola de ficheros pendientes de procesar, en Redis.
//
// n8n manda el .gpx, la API lo encola y contesta enseguida; el procesado —parsear,
// resolver el vendedor, volcar los puntos, recalcular el día— ocurre después en
// el proceso de ingesta. Así una subida masiva no deja a n8n esperando ni bloquea
// el panel.
//
// TODAS las claves llevan el prefijo `procovar-rutas:`, para no mezclarse con las
// de PEDIDO (`procovar-pedido:*`) ni con las de los demás sistemas que comparten
// el mismo Redis.
package cola

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const PrefijoPorDefecto = "procovar-rutas:"

// Trabajo es un fichero esperando turno.
type Trabajo struct {
	FuenteID        string    `json:"fuenteId,omitempty"`
	FolderID        string    `json:"folderId,omitempty"`
	DriveFileID     string    `json:"driveFileId"`
	Nombre          string    `json:"name"`
	RutaCarpeta     []string  `json:"rutaCarpeta,omitempty"`
	Creado          time.Time `json:"createdAt"`
	ContenidoBase64 string    `json:"contenidoBase64"`
	Encolado        time.Time `json:"queued"`
	// Intentos cuenta las veces que se ha reintentado, para no reintentar sin fin.
	Intentos int `json:"attempts"`
}

type Cola struct {
	rdb     *redis.Client
	prefijo string
}

func Nueva(url, prefijo string) (*Cola, error) {
	if prefijo == "" {
		prefijo = PrefijoPorDefecto
	}
	// Que el prefijo termine en ":" no es cosmético: sin él, `procovar-rutas` y
	// `procovar-rutas-viejo` compartirían espacio de claves.
	if !strings.HasSuffix(prefijo, ":") {
		prefijo += ":"
	}

	opciones, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	return &Cola{rdb: redis.NewClient(opciones), prefijo: prefijo}, nil
}

func (c *Cola) clave(nombre string) string { return c.prefijo + nombre }

func (c *Cola) pendientes() string { return c.clave("ingesta:pendientes") }
func (c *Cola) procesando() string { return c.clave("ingesta:procesando") }
func (c *Cola) fallidos() string   { return c.clave("ingesta:fallidos") }

func (c *Cola) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }
func (c *Cola) Cerrar() error                  { return c.rdb.Close() }

// Encolar mete un fichero al final de la cola.
func (c *Cola) Encolar(ctx context.Context, t Trabajo) error {
	if t.Encolado.IsZero() {
		t.Encolado = time.Now().UTC()
	}
	datos, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.LPush(ctx, c.pendientes(), datos).Err()
}

// Tomar saca el siguiente trabajo, esperando hasta `espera` si no hay ninguno.
//
// Usa BRPOPLPUSH: el trabajo pasa a una lista "procesando" en la misma
// operación, así que si el proceso se cae a mitad, el fichero sigue ahí y se
// puede recuperar. Con un RPOP normal, un reinicio en el momento justo perdería
// el recorrido de ese día para siempre.
func (c *Cola) Tomar(ctx context.Context, espera time.Duration) (*Trabajo, string, error) {
	crudo, err := c.rdb.BRPopLPush(ctx, c.pendientes(), c.procesando(), espera).Result()
	if err == redis.Nil {
		return nil, "", nil // no había nada
	}
	if err != nil {
		return nil, "", err
	}

	var t Trabajo
	if err := json.Unmarshal([]byte(crudo), &t); err != nil {
		// Un elemento ilegible no puede quedarse dando vueltas: se aparta.
		c.rdb.LRem(ctx, c.procesando(), 1, crudo)
		c.rdb.LPush(ctx, c.fallidos(), crudo)
		return nil, "", fmt.Errorf("trabajo ilegible, apartado: %w", err)
	}
	return &t, crudo, nil
}

// Terminar da por bueno un trabajo y lo quita de "procesando".
func (c *Cola) Terminar(ctx context.Context, crudo string) error {
	return c.rdb.LRem(ctx, c.procesando(), 1, crudo).Err()
}

// MaxIntentos: a partir de aquí el fichero se aparta en vez de reintentarse.
// Si tres veces no ha entrado, no va a entrar por insistir: lo que hace falta es
// que alguien lo mire.
const MaxIntentos = 3

// Fallar devuelve el trabajo a la cola para reintentarlo, o lo aparta si ya se
// intentó demasiadas veces.
func (c *Cola) Fallar(ctx context.Context, crudo string, t Trabajo) error {
	if err := c.rdb.LRem(ctx, c.procesando(), 1, crudo).Err(); err != nil {
		return err
	}

	t.Intentos++
	if t.Intentos >= MaxIntentos {
		return c.rdb.LPush(ctx, c.fallidos(), crudo).Err()
	}

	datos, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.LPush(ctx, c.pendientes(), datos).Err()
}

// Recuperar devuelve a la cola lo que quedó a medias en un reinicio.
// Se llama al arrancar la ingesta.
func (c *Cola) Recuperar(ctx context.Context) (int, error) {
	n := 0
	for {
		crudo, err := c.rdb.RPopLPush(ctx, c.procesando(), c.pendientes()).Result()
		if err == redis.Nil {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		_ = crudo
		n++
	}
}

// Estado es lo que enseña la pantalla de administración.
type Estado struct {
	Pendientes int64 `json:"pending"`
	Procesando int64 `json:"processing"`
	Fallidos   int64 `json:"failed"`
}

func (c *Cola) Estado(ctx context.Context) (Estado, error) {
	var e Estado
	var err error
	if e.Pendientes, err = c.rdb.LLen(ctx, c.pendientes()).Result(); err != nil {
		return e, err
	}
	if e.Procesando, err = c.rdb.LLen(ctx, c.procesando()).Result(); err != nil {
		return e, err
	}
	if e.Fallidos, err = c.rdb.LLen(ctx, c.fallidos()).Result(); err != nil {
		return e, err
	}
	return e, nil
}
