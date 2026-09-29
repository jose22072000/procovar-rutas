package api

import (
	"math"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

// ADELGAZAR EL RECORRIDO ANTES DE MANDARLO AL VISOR.
//
// El GPS de la fuerza de venta escribe un punto por segundo. Una jornada de ANDY el
// 26/09/2026 son 24.924 puntos (3,4 MB de JSON) y un día completo 80.935 (11 MB).
// Esos megabytes viajan por Cloudflare y por la conexión de Cuba en cada apertura del
// visor, y el navegador los tiene que convertir en dibujo.
//
// Y no hacen falta: a un punto por segundo, un vendedor parado en un semáforo escribe
// sesenta puntos en el mismo metro cuadrado, y una recta de cinco minutos por la
// carretera son trescientos puntos alineados. La línea que se ve en pantalla es la
// misma con ~2.000.
//
// Douglas-Peucker es exactamente eso: quita el punto cuya distancia a la recta que
// une a sus vecinos sea menor que una tolerancia, y conserva SIEMPRE las esquinas —
// que son las que dicen por dónde giró. Los extremos (el primero y el último punto
// del día) nunca se tocan, porque son «empezó aquí» y «terminó aquí» en el mapa.
//
// Lo que NO toca:
//   - La base de datos. Los puntos siguen enteros en `track_point`; esto es solo lo
//     que se manda a dibujar.
//   - El reporte (`report.go`), que calcula kilómetros y tramos de movimiento y SÍ
//     necesita todos los puntos. Por eso esto vive aquí y no dentro de la consulta.
//   - Los kilómetros ni la cobertura del día, que se calcularon en la ingesta con
//     todos los puntos y viven en `track_day`.
const maxPuntosVisor = 2000

// simplificarPuntos deja como mucho `maximo` puntos conservando la forma del
// recorrido. Devuelve la misma rebanada si ya viene por debajo del tope.
//
// La tolerancia no se fija a ojo: se empieza en un metro y se va doblando hasta que
// el resultado cabe. Así un día de ciudad —lleno de esquinas— se queda con una
// tolerancia fina, y un día de carretera —casi todo rectas— tolera más sin perder
// nada que se vea.
func simplificarPuntos(ps []store.DayPointsRow, maximo int) []store.DayPointsRow {
	if maximo < 2 || len(ps) <= maximo {
		return ps
	}

	// Coordenadas proyectadas a metros UNA vez, y no dentro del bucle: a esta escala
	// —un día de trabajo, decenas de kilómetros— la tierra es plana de sobra, y hacer
	// trigonometría por cada comparación multiplicaría el coste por nada.
	n := len(ps)
	xs := make([]float64, n)
	ys := make([]float64, n)
	lat0 := rad(ps[0].Lat)
	const mPorGradoLat = 110574.0
	mPorGradoLon := 111320.0 * math.Cos(lat0)
	for i, p := range ps {
		xs[i] = (p.Lon - ps[0].Lon) * mPorGradoLon
		ys[i] = (p.Lat - ps[0].Lat) * mPorGradoLat
	}

	guardar := make([]bool, n)
	for tol := 1.0; tol <= 4096; tol *= 2 {
		douglasPeucker(xs, ys, tol, guardar)
		cuantos := 0
		for _, v := range guardar {
			if v {
				cuantos++
			}
		}
		if cuantos <= maximo {
			out := make([]store.DayPointsRow, 0, cuantos)
			for i, v := range guardar {
				if v {
					out = append(out, ps[i])
				}
			}
			return out
		}
	}

	// Cuatro kilómetros de tolerancia y todavía sobran puntos: eso no es un recorrido,
	// es un día imposible. Antes de mandar once megabytes se corta a lo bruto, uno de
	// cada k, que al menos dibuja algo y llega.
	paso := (n + maximo - 1) / maximo
	out := make([]store.DayPointsRow, 0, maximo+1)
	ultimo := -1
	for i := 0; i < n; i += paso {
		out = append(out, ps[i])
		ultimo = i
	}
	if ultimo != n-1 {
		out = append(out, ps[n-1])
	}
	return out
}

// douglasPeucker marca en `guardar` los puntos que sobreviven a la tolerancia `tol`
// (en metros). Va con una pila propia y no con recursión: un día completo son 80.000
// puntos y una ruta casi recta lleva la recursión a esa misma profundidad.
func douglasPeucker(xs, ys []float64, tol float64, guardar []bool) {
	n := len(xs)
	for i := range guardar {
		guardar[i] = false
	}
	if n == 0 {
		return
	}
	guardar[0] = true
	guardar[n-1] = true
	if n < 3 {
		return
	}

	type tramo struct{ a, b int }
	pila := []tramo{{0, n - 1}}
	for len(pila) > 0 {
		t := pila[len(pila)-1]
		pila = pila[:len(pila)-1]
		if t.b-t.a < 2 {
			continue
		}

		// El punto más alejado de la recta a–b. Se compara el numerador al cuadrado
		// contra tol²·|ab|² para no sacar una raíz por punto.
		dx := xs[t.b] - xs[t.a]
		dy := ys[t.b] - ys[t.a]
		den := dx*dx + dy*dy

		peor, peorD := -1, 0.0
		for i := t.a + 1; i < t.b; i++ {
			var d float64
			if den == 0 {
				// a y b son el mismo sitio: el vendedor volvió al punto de partida.
				// La «recta» no existe, así que la distancia es al propio punto.
				ex := xs[i] - xs[t.a]
				ey := ys[i] - ys[t.a]
				d = (ex*ex + ey*ey) * 1 // ya es distancia² en metros²
				if d > peorD {
					peor, peorD = i, d
				}
				continue
			}
			num := dx*(ys[t.a]-ys[i]) - dy*(xs[t.a]-xs[i])
			d = num * num / den
			if d > peorD {
				peor, peorD = i, d
			}
		}

		if peor >= 0 && peorD > tol*tol {
			guardar[peor] = true
			pila = append(pila, tramo{t.a, peor}, tramo{peor, t.b})
		}
	}
}

func rad(g float64) float64 { return g * math.Pi / 180 }
