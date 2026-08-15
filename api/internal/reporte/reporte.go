// Package reporte arma el documento semanal por vendedor.
//
// Lo que pediste: de lunes a viernes, y por cada día la tabla con CADA
// movimiento entre las 9:00 y las 16:00, en filas que alternan desplazamiento y
// parada.
//
// El armado es una función pura sobre lo que ya está en la base — no vuelve a
// calcular kilómetros ni a detectar paradas. Si el reporte recalculara por su
// cuenta, acabaría enseñando números distintos de los del panel para el mismo
// día, que es la forma más rápida de que nadie se fíe de ninguno de los dos.
package reporte

import (
	"fmt"
	"math"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/almacen"
	"github.com/procovar/procovar-rutas/api/internal/metricas"
)

type Cabecera struct {
	Vendedor   string `json:"vendedor"`
	VendedorID string `json:"vendedorId"`
	Desde      string `json:"desde"`
	Hasta      string `json:"hasta"`
	Jornada    string `json:"jornada"`
	Zona       string `json:"zona"`
}

// Movimiento es una fila de la tabla del día.
type Movimiento struct {
	// Tipo: "desplazamiento" o "parada".
	Tipo        string  `json:"tipo"`
	HoraInicio  string  `json:"horaInicio"`
	HoraFin     string  `json:"horaFin"`
	DuracionMin int     `json:"duracionMin"`
	DistanciaKm float64 `json:"distanciaKm"`
	VelMedia    float64 `json:"velMedia"`
	VelMaxima   float64 `json:"velMaxima"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	// Lugar es el cliente más cercano si se cruzó con la geo de clientes.
	Lugar string `json:"lugar,omitempty"`
}

type Dia struct {
	Fecha  string `json:"fecha"`
	Estado string `json:"estado"`
	// Motivo explica en palabras por qué el día no es bueno. Un día malo lleva
	// su sección igual que uno bueno: es el que hay que enseñar.
	Motivo        string       `json:"motivo,omitempty"`
	PrimerFix     string       `json:"primerFix,omitempty"`
	UltimoFix     string       `json:"ultimoFix,omitempty"`
	KmNetos       float64      `json:"kmNetos"`
	Cobertura     float64      `json:"cobertura"`
	MinParado     int          `json:"minParado"`
	MinMovimiento int          `json:"minMovimiento"`
	Banderas      []string     `json:"banderas"`
	Movimientos   []Movimiento `json:"movimientos"`
	// Recorrido son los puntos para pintar el mapa de la sección.
	Recorrido []Punto `json:"recorrido"`
	Lugar     string  `json:"lugar,omitempty"`
}

type Punto struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Ts  string  `json:"ts,omitempty"`
}

type Resumen struct {
	DiasOK            int     `json:"diasOk"`
	DiasSinFichero    int     `json:"diasSinFichero"`
	DiasSinFecha      int     `json:"diasSinFecha"`
	DiasSinMovimiento int     `json:"diasSinMovimiento"`
	KmTotal           float64 `json:"kmTotal"`
	Paradas           int     `json:"paradas"`
	CoberturaMedia    float64 `json:"coberturaMedia"`
}

type Documento struct {
	Cabecera Cabecera `json:"cabecera"`
	Resumen  Resumen  `json:"resumen"`
	Dias     []Dia    `json:"dias"`
}

var motivos = map[string]string{
	"SIN_FICHERO":       "No se subió ningún fichero de recorrido este día.",
	"SIN_FECHA":         "El fichero llegó sin horas, así que no se pudo medir la jornada.",
	"SIN_MOVIMIENTO":    "Los puntos no se alejaron del mismo lugar en toda la jornada.",
	"MOVIMIENTO_ESCASO": "Se movió, pero muy por debajo de una ruta normal.",
	"NO_LABORABLE":      "Día no laborable.",
}

// DiaVacio es un día laborable del que no hay ni fila en la base.
func DiaVacio(fecha string) Dia {
	return Dia{
		Fecha:       fecha,
		Estado:      "SIN_FICHERO",
		Motivo:      motivos["SIN_FICHERO"],
		Banderas:    []string{},
		Movimientos: []Movimiento{},
		Recorrido:   []Punto{},
	}
}

// ArmarDia construye la sección de un día: la tabla de movimientos alternando
// desplazamientos y paradas, más los puntos del mapa.
func ArmarDia(
	d almacen.TrackDay,
	puntos []almacen.PuntosDeDiaRow,
	paradas []almacen.Stop,
	zona *time.Location,
) Dia {
	dia := Dia{
		Fecha:         d.Fecha.Format("2006-01-02"),
		Estado:        string(d.Estado),
		Motivo:        motivos[string(d.Estado)],
		KmNetos:       d.KmNetos,
		Cobertura:     d.Cobertura,
		MinParado:     int(d.MinParado),
		MinMovimiento: int(d.MinMovimiento),
		Banderas:      d.Banderas,
		Movimientos:   []Movimiento{},
		Recorrido:     make([]Punto, 0, len(puntos)),
	}
	if d.LugarTexto != nil {
		dia.Lugar = *d.LugarTexto
	}
	if d.PrimerFix != nil {
		dia.PrimerFix = d.PrimerFix.In(zona).Format("15:04")
	}
	if d.UltimoFix != nil {
		dia.UltimoFix = d.UltimoFix.In(zona).Format("15:04")
	}

	for _, p := range puntos {
		pt := Punto{Lat: p.Lat, Lon: p.Lon}
		if p.Ts != nil {
			pt.Ts = p.Ts.In(zona).Format("15:04:05")
		}
		dia.Recorrido = append(dia.Recorrido, pt)
	}

	dia.Movimientos = movimientos(puntos, paradas, zona)
	return dia
}

// movimientos intercala paradas y desplazamientos en orden cronológico.
//
// Los tramos de desplazamiento son "lo que queda entre dos paradas": se
// calculan por diferencia y no por su cuenta, para que la suma de la tabla
// cuadre siempre con los totales del día.
func movimientos(
	puntos []almacen.PuntosDeDiaRow,
	paradas []almacen.Stop,
	zona *time.Location,
) []Movimiento {
	out := []Movimiento{}
	if len(puntos) == 0 {
		return out
	}

	corte := 0
	for _, p := range paradas {
		// Tramo antes de esta parada.
		ini := corte
		for corte < len(puntos) && puntos[corte].Ts != nil && puntos[corte].Ts.Before(p.Inicio) {
			corte++
		}
		if m, ok := tramo(puntos[ini:corte], zona); ok {
			out = append(out, m)
		}

		out = append(out, Movimiento{
			Tipo:        "parada",
			HoraInicio:  p.Inicio.In(zona).Format("15:04"),
			HoraFin:     p.Fin.In(zona).Format("15:04"),
			DuracionMin: int(p.DuracionMin),
			Lat:         p.Lat,
			Lon:         p.Lon,
			Lugar:       lugarDeParada(p),
		})

		// Saltar los puntos que caen dentro de la parada.
		for corte < len(puntos) && puntos[corte].Ts != nil && !puntos[corte].Ts.After(p.Fin) {
			corte++
		}
	}

	// Lo que quede después de la última parada.
	if m, ok := tramo(puntos[corte:], zona); ok {
		out = append(out, m)
	}

	return out
}

func tramo(puntos []almacen.PuntosDeDiaRow, zona *time.Location) (Movimiento, bool) {
	if len(puntos) < 2 || puntos[0].Ts == nil || puntos[len(puntos)-1].Ts == nil {
		return Movimiento{}, false
	}

	var metros, velMax float64
	for i := 1; i < len(puntos); i++ {
		metros += metricas.DistanciaM(
			metricas.Coord{Lat: puntos[i-1].Lat, Lon: puntos[i-1].Lon},
			metricas.Coord{Lat: puntos[i].Lat, Lon: puntos[i].Lon})
		if puntos[i].Speed != nil && *puntos[i].Speed > velMax {
			velMax = *puntos[i].Speed
		}
	}

	inicio := *puntos[0].Ts
	fin := *puntos[len(puntos)-1].Ts
	minutos := int(fin.Sub(inicio).Minutes())

	km := redondear(metros/1000, 2)
	media := 0.0
	if minutos > 0 {
		media = redondear(km/(float64(minutos)/60), 1)
	}

	return Movimiento{
		Tipo:        "desplazamiento",
		HoraInicio:  inicio.In(zona).Format("15:04"),
		HoraFin:     fin.In(zona).Format("15:04"),
		DuracionMin: minutos,
		DistanciaKm: km,
		VelMedia:    media,
		VelMaxima:   redondear(velMax, 1),
		Lat:         puntos[len(puntos)-1].Lat,
		Lon:         puntos[len(puntos)-1].Lon,
	}, true
}

func lugarDeParada(p almacen.Stop) string {
	if p.ClienteNombre == nil {
		return ""
	}
	if p.DistanciaClienteM != nil {
		return fmt.Sprintf("a %.0f m de %s", *p.DistanciaClienteM, *p.ClienteNombre)
	}
	return *p.ClienteNombre
}

// Armar cierra el documento con su resumen.
func Armar(cab Cabecera, dias []Dia) Documento {
	doc := Documento{Cabecera: cab, Dias: dias}

	conDatos := 0
	for _, d := range dias {
		switch d.Estado {
		case "OK":
			doc.Resumen.DiasOK++
		case "SIN_FICHERO":
			doc.Resumen.DiasSinFichero++
		case "SIN_FECHA":
			doc.Resumen.DiasSinFecha++
		case "SIN_MOVIMIENTO":
			doc.Resumen.DiasSinMovimiento++
		}
		doc.Resumen.KmTotal += d.KmNetos
		for _, m := range d.Movimientos {
			if m.Tipo == "parada" {
				doc.Resumen.Paradas++
			}
		}
		if d.Cobertura > 0 {
			doc.Resumen.CoberturaMedia += d.Cobertura
			conDatos++
		}
	}

	doc.Resumen.KmTotal = redondear(doc.Resumen.KmTotal, 2)
	if conDatos > 0 {
		doc.Resumen.CoberturaMedia = redondear(doc.Resumen.CoberturaMedia/float64(conDatos), 1)
	}

	return doc
}

func redondear(v float64, decimales int) float64 {
	f := math.Pow(10, float64(decimales))
	return math.Round(v*f) / f
}
