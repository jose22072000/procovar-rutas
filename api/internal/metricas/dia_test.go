package metricas

import (
	"testing"
	"time"
)

// Cuba está en UTC-4 en agosto: las 13:00 UTC son las 09:00 de la mañana allí.
// El 10 de agosto de 2026 es lunes.
func hora(h, m int) *time.Time {
	t := time.Date(2026, 8, 10, h+4, m, 0, 0, time.UTC)
	return &t
}

func punto(lat, lon float64, t *time.Time, seq int) PuntoEntrada {
	return PuntoEntrada{Lat: lat, Lon: lon, Ts: t, Seq: seq}
}

// rutaMoviendose avanza en línea recta: ~1 km por cada 0,01° de longitud aquí.
func rutaMoviendose(desde, hasta, pasoMin int) []PuntoEntrada {
	puntos := []PuntoEntrada{}
	i := 0
	for min := desde * 60; min <= hasta*60; min += pasoMin {
		puntos = append(puntos, punto(21.38, -77.91+float64(i)*0.01, hora(min/60, min%60), i))
		i++
	}
	return puntos
}

// rutaQuieta es un teléfono inmóvil: solo el temblor del GPS.
// amplitud 0.0002° ≈ 22 m, que es el peor caso realista en ciudad.
func rutaQuieta(desde, hasta, pasoMin int, amplitud float64) []PuntoEntrada {
	puntos := []PuntoEntrada{}
	i := 0
	for min := desde * 60; min <= hasta*60; min += pasoMin {
		ruido := amplitud
		if i%2 == 1 {
			ruido = -amplitud
		}
		puntos = append(puntos, punto(21.38+ruido, -77.91-ruido, hora(min/60, min%60), i))
		i++
	}
	return puntos
}

func TestEvaluarMarcaSaltosSinEncadenarElError(t *testing.T) {
	cfg := ConfigPorDefecto()
	puntos := EvaluarPuntos([]PuntoEntrada{
		punto(21.38, -77.91, hora(9, 0), 0),
		punto(30.0, -80.0, hora(9, 1), 1), // cientos de km en un minuto
		punto(21.381, -77.911, hora(9, 2), 2),
	}, cfg)

	if puntos[1].Quality != CalidadSalto {
		t.Errorf("el rebote debería marcarse como SALTO, es %s", puntos[1].Quality)
	}
	// El punto bueno posterior se compara con el bueno anterior, no con el rebote.
	if puntos[2].Quality != CalidadOK {
		t.Errorf("el punto siguiente debería seguir siendo OK, es %s", puntos[2].Quality)
	}
}

func TestEvaluarMarcaDuplicadosImprecisosYSinHora(t *testing.T) {
	cfg := ConfigPorDefecto()
	mala := 500.0
	puntos := EvaluarPuntos([]PuntoEntrada{
		punto(21.38, -77.91, hora(9, 0), 0),
		punto(21.38, -77.91, hora(9, 0), 1),
		{Lat: 21.382, Lon: -77.912, Ts: hora(9, 5), Accuracy: &mala, Seq: 2},
		punto(21.383, -77.913, nil, 3),
	}, cfg)

	encontradas := map[Calidad]bool{}
	for _, p := range puntos {
		encontradas[p.Quality] = true
	}
	for _, q := range []Calidad{CalidadDuplicado, CalidadImpreciso, CalidadSinHora} {
		if !encontradas[q] {
			t.Errorf("falta la calidad %s", q)
		}
	}
}

func TestDiaNormalSaleOK(t *testing.T) {
	r := CalcularDia(rutaMoviendose(9, 16, 5), ConfigPorDefecto())

	if r.Estado != DiaOK {
		t.Errorf("estado = %s, se esperaba OK", r.Estado)
	}
	if r.KmNetos < 5 {
		t.Errorf("km = %v, se esperaba una ruta de verdad", r.KmNetos)
	}
	if r.Cobertura < 90 {
		t.Errorf("cobertura = %v", r.Cobertura)
	}
}

func TestSinPuntosEsSinFichero(t *testing.T) {
	if r := CalcularDia(nil, ConfigPorDefecto()); r.Estado != DiaSinFichero {
		t.Errorf("estado = %s", r.Estado)
	}
}

// Coordenadas sin horas: ni día bueno ni ausencia. Su propio caso.
func TestSinHorasEsSinFechaYNoAusencia(t *testing.T) {
	r := CalcularDia([]PuntoEntrada{
		punto(21.38, -77.91, nil, 0),
		punto(21.39, -77.92, nil, 1),
	}, ConfigPorDefecto())

	if r.Estado != DiaSinFecha {
		t.Errorf("estado = %s, se esperaba SIN_FECHA", r.Estado)
	}
	if r.Centroide == nil {
		t.Error("debería poder pintarse en el mapa igualmente")
	}
}

func TestDiaEnteroSinMoverse(t *testing.T) {
	r := CalcularDia(rutaQuieta(9, 16, 5, 0.00007), ConfigPorDefecto()) // ruido de ~8 m

	if r.Estado != DiaSinMovimiento {
		t.Errorf("estado = %s, se esperaba SIN_MOVIMIENTO", r.Estado)
	}
	if r.RadioDispersion >= 300 {
		t.Errorf("radio = %v", r.RadioDispersion)
	}
	if r.Centroide == nil {
		t.Error("hace falta saber DÓNDE pasó el día quieto")
	}
}

// El caso que hundió la primera versión de la regla: con fixes que bailan 22 m
// muestreando cada minuto, el ruido acumulado pasa de 5 km. Si la inmovilidad se
// decidiera por kilómetros, este día se colaría como trabajado.
func TestRuidoDelGpsNoDisfrazaLaInmovilidad(t *testing.T) {
	r := CalcularDia(rutaQuieta(9, 16, 1, 0.0002), ConfigPorDefecto())

	if r.Estado != DiaSinMovimiento {
		t.Errorf("estado = %s, se esperaba SIN_MOVIMIENTO pese al ruido", r.Estado)
	}
	if r.RadioDispersion >= 300 {
		t.Errorf("radio = %v: nunca salió de la manzana", r.RadioDispersion)
	}
}

// Y el simétrico: una jornada corta y quieta es poca cobertura, no una
// acusación de que se pasó el día sin moverse.
func TestJornadaDemasiadoCortaNoAcusaDeInmovilidad(t *testing.T) {
	r := CalcularDia(rutaQuieta(9, 10, 5, 0.00007), ConfigPorDefecto())

	if r.Estado == DiaSinMovimiento {
		t.Error("con una hora de datos no se puede afirmar que no se movió")
	}
	if !tiene(r.Banderas, BanderaPocaCobertura) {
		t.Errorf("banderas = %v, faltaba poca_cobertura", r.Banderas)
	}
}

func TestMovimientoEscaso(t *testing.T) {
	puntos := []PuntoEntrada{}
	for i := 0; i <= 28; i++ {
		// ~3 km en toda la jornada: se movió, pero poco.
		puntos = append(puntos, punto(21.38+float64(i)*0.001, -77.91, hora(9+i/4, (i%4)*15), i))
	}
	r := CalcularDia(puntos, ConfigPorDefecto())

	if r.Estado != DiaMovimientoEscaso {
		t.Errorf("estado = %s, se esperaba MOVIMIENTO_ESCASO", r.Estado)
	}
}

func TestBanderasDeEntradaYSalida(t *testing.T) {
	r := CalcularDia(rutaMoviendose(11, 14, 5), ConfigPorDefecto())

	for _, b := range []string{BanderaEntradaTardia, BanderaSalidaTemprana, BanderaPocaCobertura} {
		if !tiene(r.Banderas, b) {
			t.Errorf("falta la bandera %s en %v", b, r.Banderas)
		}
	}
}

func TestHuecosLargosDeSenal(t *testing.T) {
	r := CalcularDia([]PuntoEntrada{
		punto(21.38, -77.91, hora(9, 0), 0),
		punto(21.39, -77.92, hora(9, 10), 1),
		punto(21.5, -77.95, hora(13, 0), 2), // casi cuatro horas sin señal
		punto(21.51, -77.96, hora(13, 10), 3),
	}, ConfigPorDefecto())

	if r.Huecos == 0 || !tiene(r.Banderas, BanderaHuecoLargo) {
		t.Errorf("huecos = %d, banderas = %v", r.Huecos, r.Banderas)
	}
}

// La jornada es 9–16: un viaje de madrugada no convierte un día quieto en un día
// trabajado.
func TestSoloCuentaLaJornada(t *testing.T) {
	puntos := []PuntoEntrada{
		punto(21.0, -77.0, hora(2, 0), 0),
		punto(21.2, -77.4, hora(3, 0), 1),
	}
	puntos = append(puntos, rutaQuieta(9, 16, 5, 0.00007)...)

	r := CalcularDia(puntos, ConfigPorDefecto())
	if r.Estado != DiaSinMovimiento {
		t.Errorf("estado = %s", r.Estado)
	}
	if r.KmNetos > 1 {
		t.Errorf("km = %v: el viaje de madrugada no debería contar", r.KmNetos)
	}
}

func TestParadaNoSePartEnVarias(t *testing.T) {
	puntos := []PuntoEntrada{}
	// 09:00–09:30 quieto en un cliente, con el baile normal del GPS en ciudad.
	for i := 0; i <= 6; i++ {
		ruido := 0.0002
		if i%2 == 1 {
			ruido = -0.0002
		}
		puntos = append(puntos, punto(21.38+ruido, -77.91+ruido, hora(9, i*5), i))
	}
	// Y luego se va.
	for i := 1; i <= 6; i++ {
		puntos = append(puntos, punto(21.38+float64(i)*0.01, -77.91, hora(9, 30+i*5), 6+i))
	}

	r := CalcularDia(puntos, ConfigPorDefecto())
	if len(r.Paradas) != 1 {
		t.Fatalf("paradas = %d, se esperaba 1 (una visita, no tres)", len(r.Paradas))
	}
	if r.Paradas[0].DuracionMin != 30 {
		t.Errorf("duración = %d min", r.Paradas[0].DuracionMin)
	}
}

func tiene(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
