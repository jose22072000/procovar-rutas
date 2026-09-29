package api

import (
	"math"
	"testing"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

func punto(lat, lon float64, seg int) store.DayPointsRow {
	t := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC).Add(time.Duration(seg) * time.Second)
	return store.DayPointsRow{Ts: &t, Lat: lat, Lon: lon, Seq: int32(seg)}
}

// Por debajo del tope no se toca nada: un día de 40 puntos tiene que llegar con sus
// 40 puntos, sin que la simplificación le quite ni uno.
func TestSimplificarNoTocaLoQueYaCabe(t *testing.T) {
	var ps []store.DayPointsRow
	for i := 0; i < 40; i++ {
		ps = append(ps, punto(21.38+float64(i)*0.001, -77.91, i))
	}
	out := simplificarPuntos(ps, 2000)
	if len(out) != 40 {
		t.Fatalf("esperaba 40 puntos intactos, llegaron %d", len(out))
	}
}

// Una recta de 10.000 puntos es una recta: dos vértices la dibujan igual. Esto es el
// caso del vendedor en carretera, y es donde se gana el grueso de los megabytes.
func TestSimplificarUnaRectaSeQuedaEnDos(t *testing.T) {
	var ps []store.DayPointsRow
	for i := 0; i < 10000; i++ {
		ps = append(ps, punto(21.38+float64(i)*0.00001, -77.91, i))
	}
	out := simplificarPuntos(ps, 2000)
	if len(out) != 2 {
		t.Fatalf("una recta tenía que quedar en 2 puntos, quedó en %d", len(out))
	}
}

// Los extremos son «empezó aquí» y «terminó aquí»: el visor los pinta con su globo y
// su hora. Nunca pueden desaparecer.
func TestSimplificarConservaLosExtremos(t *testing.T) {
	ps := rutaDeCiudad(30000)
	out := simplificarPuntos(ps, 2000)
	if out[0] != ps[0] {
		t.Fatalf("se perdió el primer punto del día")
	}
	if out[len(out)-1] != ps[len(ps)-1] {
		t.Fatalf("se perdió el último punto del día")
	}
}

// El tope se respeta de verdad, y el orden por tiempo se mantiene: el deslizador de
// la línea de tiempo recorre esta lista tal cual.
func TestSimplificarRespetaElTopeYElOrden(t *testing.T) {
	ps := rutaDeCiudad(80935) // el día completo de ANDY, 26/09/2026
	out := simplificarPuntos(ps, 2000)
	if len(out) > 2000 {
		t.Fatalf("pasó del tope: %d puntos", len(out))
	}
	if len(out) < 100 {
		t.Fatalf("se pasó de frenada, solo quedaron %d puntos", len(out))
	}
	for i := 1; i < len(out); i++ {
		if !out[i-1].Ts.Before(*out[i].Ts) {
			t.Fatalf("el punto %d va hacia atrás en el tiempo", i)
		}
		if out[i-1].Seq >= out[i].Seq {
			t.Fatalf("el punto %d rompe el orden de seq", i)
		}
	}
}

// Lo que importa de verdad: la línea que queda tiene que ser LA MISMA línea. Se
// comprueba midiendo cuánto se aparta cada punto original del recorrido simplificado.
// Si esto se rompiera, el mapa enseñaría una ruta que el vendedor no hizo.
func TestSimplificarNoDeformaElRecorrido(t *testing.T) {
	ps := rutaDeCiudad(30000)
	out := simplificarPuntos(ps, 2000)

	peor := 0.0
	for _, p := range ps {
		d := distanciaAPolilinea(p, out)
		if d > peor {
			peor = d
		}
	}
	// Medido el 29/09/2026 sobre esta misma ruta: 30.000 puntos se quedan en ~1.540 y
	// el desvío máximo es de 4,00 m — menos de lo que el propio GPS tiembla, y mucho
	// menos que el grosor de la línea dibujada. El listón se pone en 8 m para que un
	// cambio que afloje la tolerancia se caiga aquí y no en el mapa de Jose.
	if peor > 8 {
		t.Fatalf("el recorrido simplificado se aparta %.1f m del real", peor)
	}
}

// Una esquina es la información: dice por dónde giró. Una ruta en forma de cuadrado
// tiene que conservar sus cuatro esquinas por muchos puntos que lleve cada lado.
func TestSimplificarConservaLasEsquinas(t *testing.T) {
	esquinas := [][2]float64{{21.380, -77.910}, {21.380, -77.890}, {21.400, -77.890}, {21.400, -77.910}, {21.380, -77.910}}
	var ps []store.DayPointsRow
	seg := 0
	for i := 0; i < len(esquinas)-1; i++ {
		a, b := esquinas[i], esquinas[i+1]
		for k := 0; k < 3000; k++ {
			f := float64(k) / 3000
			ps = append(ps, punto(a[0]+(b[0]-a[0])*f, a[1]+(b[1]-a[1])*f, seg))
			seg++
		}
	}
	ps = append(ps, punto(esquinas[len(esquinas)-1][0], esquinas[len(esquinas)-1][1], seg))

	out := simplificarPuntos(ps, 2000)
	for _, e := range esquinas {
		cerca := false
		for _, p := range out {
			if math.Abs(p.Lat-e[0]) < 1e-5 && math.Abs(p.Lon-e[1]) < 1e-5 {
				cerca = true
				break
			}
		}
		if !cerca {
			t.Fatalf("se perdió la esquina %v: el mapa dibujaría un giro que no existe", e)
		}
	}
}

// rutaDeCiudad fabrica un recorrido parecido al real: tramos rectos con giros y el
// temblor del GPS encima, a un punto por segundo.
func rutaDeCiudad(n int) []store.DayPointsRow {
	ps := make([]store.DayPointsRow, 0, n)
	lat, lon := 21.380, -77.910
	rumbo := 0.0
	for i := 0; i < n; i++ {
		if i%400 == 0 {
			rumbo += math.Pi / 2 // una esquina cada 400 segundos
		}
		// Y las calles no son rectas: una curva lenta y continua encima del rumbo. Sin
		// esto la ruta de prueba sería un trazo de regla, que cualquier tolerancia por
		// gruesa que fuera dibujaría igual de bien, y la prueba no comprobaría nada.
		rumbo += math.Sin(float64(i)/900) * 0.004
		lat += math.Cos(rumbo) * 0.000045
		lon += math.Sin(rumbo) * 0.000045
		// El temblor del receptor: unos pocos metros que no son movimiento.
		tlat := math.Sin(float64(i)*1.7) * 0.000018
		tlon := math.Cos(float64(i)*2.3) * 0.000018
		ps = append(ps, punto(lat+tlat, lon+tlon, i))
	}
	return ps
}

// distanciaAPolilinea: metros del punto p al trazo que forman los puntos de la línea.
func distanciaAPolilinea(p store.DayPointsRow, linea []store.DayPointsRow) float64 {
	const mLat = 110574.0
	mLon := 111320.0 * math.Cos(rad(p.Lat))
	px, py := p.Lon*mLon, p.Lat*mLat

	mejor := math.Inf(1)
	for i := 1; i < len(linea); i++ {
		ax, ay := linea[i-1].Lon*mLon, linea[i-1].Lat*mLat
		bx, by := linea[i].Lon*mLon, linea[i].Lat*mLat
		dx, dy := bx-ax, by-ay
		den := dx*dx + dy*dy
		t := 0.0
		if den > 0 {
			t = ((px-ax)*dx + (py-ay)*dy) / den
			t = math.Max(0, math.Min(1, t))
		}
		ex, ey := px-(ax+t*dx), py-(ay+t*dy)
		if d := math.Sqrt(ex*ex + ey*ey); d < mejor {
			mejor = d
		}
	}
	return mejor
}
