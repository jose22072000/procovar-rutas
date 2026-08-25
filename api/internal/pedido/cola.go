package pedido

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// La cola de sincronización con PEDIDO, y su trabajador.
//
// # Por qué una cola, si ya había un temporizador
//
// El temporizador pedía la VENTANA ENTERA de una vez: tres semanas, miles de
// pedidos, en una sola llamada, cada hora. Eso es una ráfaga contra PEDIDO —que es
// la aplicación de la que dependen las sucursales para trabajar— y encima con el
// peor reparto posible: veintitrés minutos de nada y un golpe.
//
// Aquí el trabajo se parte en DÍAS. Cada día es un trabajo pequeño, y el trabajador
// coge uno, lo hace, ESPERA, y coge el siguiente. PEDIDO recibe una consulta chica
// cada pocos segundos en vez de un mazazo cada hora, y si hay que ponerse al día con
// un mes atrasado, se pone al día solo, sin prisa y sin que nadie lo note.
//
// # Lo que la cola garantiza
//
// Un día encolado no se encola dos veces (el conjunto `encolados`), así que el
// planificador puede pasar cada minuto sin acumular trabajo repetido. Y un trabajo
// que se estaba haciendo cuando se reinició el contenedor vuelve a la cola en vez de
// perderse, igual que con los .gpx.
//
// Las claves llevan el mismo prefijo `procovar-rutas:` que todo lo demás, así que no
// se mezclan con las de PEDIDO (`procovar-pedido:*`) en el mismo Redis.

// Trabajo es un día que hay que traer de PEDIDO y volver a cruzar.
type Trabajo struct {
	// Fecha, en AAAA-MM-DD. Un día, no un rango: es la unidad que hace que ninguna
	// consulta a PEDIDO sea grande.
	Fecha string `json:"fecha"`
	// Completo = se pide el día entero aunque no haya cambiado nada allí. El
	// incremental sólo pide lo que se movió.
	Completo bool      `json:"completo"`
	Encolado time.Time `json:"encolado"`
	Intentos int       `json:"intentos"`
}

// Cola es la cola de días pendientes.
type Cola struct {
	rdb    *redis.Client
	prefix string
}

// NuevaCola devuelve nil si no hay Redis: sin cola, el trabajador sincroniza igual,
// día a día y con la misma pausa, sólo que sin poder repartirse entre procesos ni
// sobrevivir a un reinicio a media faena.
func NuevaCola(url, prefix string) (*Cola, error) {
	if url == "" {
		return nil, nil
	}
	if prefix == "" {
		prefix = "procovar-rutas:"
	}
	if !strings.HasSuffix(prefix, ":") {
		prefix += ":"
	}
	opciones, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL inválida: %w", err)
	}
	return &Cola{rdb: redis.NewClient(opciones), prefix: prefix}, nil
}

func (c *Cola) clave(n string) string { return c.prefix + "pedidos:" + n }
func (c *Cola) pendientes() string    { return c.clave("pending") }
func (c *Cola) haciendose() string    { return c.clave("processing") }
func (c *Cola) apartados() string     { return c.clave("failed") }
func (c *Cola) encolados() string     { return c.clave("queued") }

func (c *Cola) Ping(ctx context.Context) error { return c.rdb.Ping(ctx).Err() }
func (c *Cola) Close() error                   { return c.rdb.Close() }

// Encolar pone un día al final de la cola. Si ese día ya estaba esperando, no se
// duplica: devuelve false y no hace nada.
func (c *Cola) Encolar(ctx context.Context, t Trabajo) (bool, error) {
	// El conjunto es lo que evita que el planificador, pasando cada minuto, deje
	// veinte copias del mismo día esperando turno.
	marca := t.Fecha
	if t.Completo {
		marca += ":completo"
	}
	nuevo, err := c.rdb.SAdd(ctx, c.encolados(), marca).Result()
	if err != nil {
		return false, err
	}
	if nuevo == 0 {
		return false, nil
	}

	if t.Encolado.IsZero() {
		t.Encolado = time.Now().UTC()
	}
	datos, err := json.Marshal(t)
	if err != nil {
		return false, err
	}
	if err := c.rdb.LPush(ctx, c.pendientes(), datos).Err(); err != nil {
		// Si no entró en la lista, tampoco puede quedarse marcado como encolado: si
		// no, ese día no volvería a entrar nunca.
		c.rdb.SRem(ctx, c.encolados(), marca)
		return false, err
	}
	return true, nil
}

// Tomar coge el siguiente trabajo, esperando hasta `espera` si no hay ninguno.
//
// BRPOPLPUSH, igual que la cola de ficheros: el trabajo pasa a "haciéndose" en la
// misma operación, así que si el proceso muere a medias no se pierde.
func (c *Cola) Tomar(ctx context.Context, espera time.Duration) (*Trabajo, string, error) {
	crudo, err := c.rdb.BRPopLPush(ctx, c.pendientes(), c.haciendose(), espera).Result()
	if err == redis.Nil {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}

	var t Trabajo
	if err := json.Unmarshal([]byte(crudo), &t); err != nil {
		c.rdb.LRem(ctx, c.haciendose(), 1, crudo)
		c.rdb.LPush(ctx, c.apartados(), crudo)
		return nil, "", fmt.Errorf("trabajo ilegible, apartado: %w", err)
	}
	return &t, crudo, nil
}

// Terminar da el trabajo por hecho y libera su día para que pueda volver a
// encolarse mañana.
func (c *Cola) Terminar(ctx context.Context, crudo string, t Trabajo) error {
	marca := t.Fecha
	if t.Completo {
		marca += ":completo"
	}
	c.rdb.SRem(ctx, c.encolados(), marca)
	return c.rdb.LRem(ctx, c.haciendose(), 1, crudo).Err()
}

// MaxIntentos: pasado esto el día se aparta en vez de reintentarse. Si PEDIDO no
// contesta tres veces seguidas, insistir no lo va a arreglar.
const MaxIntentos = 3

// Fallar devuelve el trabajo a la cola para otro intento, o lo aparta.
func (c *Cola) Fallar(ctx context.Context, crudo string, t Trabajo) error {
	if err := c.rdb.LRem(ctx, c.haciendose(), 1, crudo).Err(); err != nil {
		return err
	}

	marca := t.Fecha
	if t.Completo {
		marca += ":completo"
	}

	t.Intentos++
	if t.Intentos >= MaxIntentos {
		c.rdb.SRem(ctx, c.encolados(), marca)
		return c.rdb.LPush(ctx, c.apartados(), crudo).Err()
	}

	datos, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.LPush(ctx, c.pendientes(), datos).Err()
}

// Recuperar devuelve a la cola lo que un reinicio dejó a medias.
func (c *Cola) Recuperar(ctx context.Context) (int, error) {
	n := 0
	for {
		_, err := c.rdb.RPopLPush(ctx, c.haciendose(), c.pendientes()).Result()
		if err == redis.Nil {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
	}
}

// EstadoCola es lo que se puede mirar desde fuera para saber si va al día.
type EstadoCola struct {
	Pendientes int64 `json:"pendientes"`
	Haciendose int64 `json:"haciendose"`
	Apartados  int64 `json:"apartados"`
}

func (c *Cola) Estado(ctx context.Context) (EstadoCola, error) {
	var e EstadoCola
	var err error
	if e.Pendientes, err = c.rdb.LLen(ctx, c.pendientes()).Result(); err != nil {
		return e, err
	}
	if e.Haciendose, err = c.rdb.LLen(ctx, c.haciendose()).Result(); err != nil {
		return e, err
	}
	if e.Apartados, err = c.rdb.LLen(ctx, c.apartados()).Result(); err != nil {
		return e, err
	}
	return e, nil
}
