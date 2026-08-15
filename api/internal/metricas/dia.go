// Package metricas decide cómo fue el día de un vendedor.
//
// Todo lo que pinta el calendario de cumplimiento sale de aquí: los kilómetros,
// las paradas, la cobertura horaria y —lo que de verdad se quiere saber— si el
// día es OK, SIN_FECHA o SIN_MOVIMIENTO.
//
// CalcularDia es una función pura: entran unos puntos y una configuración, sale
// el veredicto. Así se prueba entera sin base de datos, y los umbrales se
// reajustan cuando lleguen los GPX reales sin tocar la ingesta.
package metricas

import (
	"math"
	"sort"
	"time"
)

// EstadoDia es el veredicto que pinta cada celda del calendario.
type EstadoDia string

const (
	DiaOK               EstadoDia = "OK"
	DiaSinFichero       EstadoDia = "SIN_FICHERO"
	DiaSinFecha         EstadoDia = "SIN_FECHA"
	DiaSinMovimiento    EstadoDia = "SIN_MOVIMIENTO"
	DiaMovimientoEscaso EstadoDia = "MOVIMIENTO_ESCASO"
	DiaNoLaborable      EstadoDia = "NO_LABORABLE"
)

// Calidad dice por qué un punto no cuenta. Se marca, NO se borra: si mañana el
// umbral resulta malo, se recalcula el día sin volver a bajar nada de Drive.
type Calidad string

const (
	CalidadOK        Calidad = "OK"
	CalidadSalto     Calidad = "SALTO"
	CalidadImpreciso Calidad = "IMPRECISO"
	CalidadDuplicado Calidad = "DUPLICADO"
	CalidadSinHora   Calidad = "SIN_HORA"
)

// Banderas que se cuelgan del día.
const (
	BanderaEntradaTardia    = "entrada_tardia"
	BanderaSalidaTemprana   = "salida_temprana"
	BanderaHuecoLargo       = "hueco_largo"
	BanderaPocaCobertura    = "poca_cobertura"
	BanderaSinMovimiento    = "sin_movimiento"
	BanderaMovimientoEscaso = "movimiento_escaso"
	BanderaSinHoras         = "sin_horas"
	BanderaSinDatosJornada  = "sin_datos_en_jornada"
)

// PuntoEntrada es un fix tal y como sale del parser.
type PuntoEntrada struct {
	Lat      float64
	Lon      float64
	Ts       *time.Time
	Accuracy *float64
	Seq      int
}

func (p PuntoEntrada) coord() Coord { return Coord{Lat: p.Lat, Lon: p.Lon} }

// PuntoEvaluado es un fix ya juzgado.
type PuntoEvaluado struct {
	PuntoEntrada
	Quality Calidad
	// Speed en km/h respecto al último punto bueno.
	Speed float64
}

// Config son los umbrales, configurables por sucursal: no es lo mismo un
// vendedor de ciudad que uno que cubre tres municipios.
type Config struct {
	Zona          *time.Location
	JornadaInicio string // "09:00"
	JornadaFin    string // "16:00"
	ParadaRadioM  float64
	ParadaMinutos int
	// PasoMinimoM: por debajo de esta distancia, un tramo es ruido y no suma km.
	PasoMinimoM  float64
	SinMovRadioM float64
	// SinMovSpanMin: jornada mínima para poder afirmar que alguien no se movió.
	SinMovSpanMin        int
	EscasoKmNetos        float64
	MaxVelocidadKmh      float64
	MaxAccuracyM         float64
	HuecoMinutos         int
	CoberturaGapMin      float64
	CoberturaMinima      float64
	ToleranciaEntradaMin int
	ToleranciaSalidaMin  int
}

// ConfigPorDefecto son los valores de partida. Se afinarán con datos reales.
func ConfigPorDefecto() Config {
	zona, err := time.LoadLocation("America/Havana")
	if err != nil {
		// Sin base de datos de zonas horarias, UTC-4 fijo. Peor (no sigue el
		// horario de verano) pero preferible a reventar.
		zona = time.FixedZone("CUT", -4*3600)
	}
	return Config{
		Zona:                 zona,
		JornadaInicio:        "09:00",
		JornadaFin:           "16:00",
		ParadaRadioM:         60,
		ParadaMinutos:        5,
		PasoMinimoM:          25,
		SinMovRadioM:         300,
		SinMovSpanMin:        120,
		EscasoKmNetos:        5,
		MaxVelocidadKmh:      150,
		MaxAccuracyM:         100,
		HuecoMinutos:         30,
		CoberturaGapMin:      5,
		CoberturaMinima:      70,
		ToleranciaEntradaMin: 15,
		ToleranciaSalidaMin:  15,
	}
}

// Parada es un intervalo en el que el vendedor no se movió del sitio.
type Parada struct {
	Inicio      time.Time
	Fin         time.Time
	DuracionMin int
	Lat         float64
	Lon         float64
	RadioM      float64
	Seq         int
}

// ResultadoDia es lo que se guarda en track_day.
type ResultadoDia struct {
	Estado          EstadoDia
	Puntos          []PuntoEvaluado
	PuntosValidos   int
	PrimerFix       *time.Time
	UltimoFix       *time.Time
	KmNetos         float64
	MinMovimiento   int
	MinParado       int
	Cobertura       float64
	Huecos          int
	RadioDispersion float64
	Centroide       *Coord
	Paradas         []Parada
	Banderas        []string
}

// HoraAMinutos convierte "09:00" en 540.
func HoraAMinutos(hhmm string) int {
	var h, m int
	if len(hhmm) >= 5 {
		h = int(hhmm[0]-'0')*10 + int(hhmm[1]-'0')
		m = int(hhmm[3]-'0')*10 + int(hhmm[4]-'0')
	}
	return h*60 + m
}

// minutosLocales devuelve los minutos desde medianoche en hora de la sucursal.
//
// Siempre vía time.Location, nunca sumando horas a mano: Cuba tiene horario de
// verano y los reportes de marzo y noviembre saldrían corridos justo en las
// semanas en que a nadie se le ocurre revisarlos.
func minutosLocales(t time.Time, zona *time.Location) int {
	l := t.In(zona)
	return l.Hour()*60 + l.Minute()
}

// EvaluarPuntos marca los puntos malos sin borrarlos.
func EvaluarPuntos(entrada []PuntoEntrada, cfg Config) []PuntoEvaluado {
	ordenados := make([]PuntoEntrada, len(entrada))
	copy(ordenados, entrada)
	sort.SliceStable(ordenados, func(i, j int) bool {
		a, b := ordenados[i], ordenados[j]
		if a.Ts != nil && b.Ts != nil {
			return a.Ts.Before(*b.Ts)
		}
		return a.Seq < b.Seq
	})

	out := make([]PuntoEvaluado, 0, len(ordenados))
	vistos := map[int64]bool{}
	var anterior *PuntoEvaluado

	for _, p := range ordenados {
		ev := PuntoEvaluado{PuntoEntrada: p, Quality: CalidadOK}

		switch {
		case p.Ts == nil:
			ev.Quality = CalidadSinHora
		case vistos[p.Ts.UnixNano()]:
			ev.Quality = CalidadDuplicado
		case p.Accuracy != nil && *p.Accuracy > cfg.MaxAccuracyM:
			ev.Quality = CalidadImpreciso
		case anterior != nil && anterior.Ts != nil:
			seg := p.Ts.Sub(*anterior.Ts).Seconds()
			ev.Speed = VelocidadKmh(anterior.coord(), p.coord(), seg)
			// Un salto imposible es un fix rebotado, no un vendedor en avión. Se
			// marca y NO se toma como referencia: si se encadenara, arrastraría
			// el error al resto de la ruta.
			if ev.Speed > cfg.MaxVelocidadKmh {
				ev.Quality = CalidadSalto
			}
		}

		if p.Ts != nil {
			vistos[p.Ts.UnixNano()] = true
		}
		out = append(out, ev)
		if ev.Quality == CalidadOK {
			ultimo := out[len(out)-1]
			anterior = &ultimo
		}
	}

	return out
}

// puntosDeJornada filtra los puntos utilizables dentro del horario laboral.
func puntosDeJornada(puntos []PuntoEvaluado, cfg Config) []PuntoEvaluado {
	ini := HoraAMinutos(cfg.JornadaInicio)
	fin := HoraAMinutos(cfg.JornadaFin)
	out := []PuntoEvaluado{}
	for _, p := range puntos {
		if p.Quality != CalidadOK || p.Ts == nil {
			continue
		}
		m := minutosLocales(*p.Ts, cfg.Zona)
		if m >= ini && m <= fin {
			out = append(out, p)
		}
	}
	return out
}

// DetectarParadas agrupa los puntos consecutivos que no se alejan del centro
// del grupo durante al menos ParadaMinutos.
//
// El radio por defecto es 60 m y no 10: en ciudad, un GPS quieto "baila" 20–30 m
// entre edificios. Con un radio pequeño, cada visita se partiría en cinco
// paradas falsas y el reporte sería ilegible.
func DetectarParadas(puntos []PuntoEvaluado, cfg Config) []Parada {
	paradas := []Parada{}
	i := 0
	seq := 0

	for i < len(puntos) {
		// El grupo crece mientras el punto siguiente esté cerca del CENTRO del
		// grupo, con el centro actualizándose sobre la marcha.
		//
		// Dos trampas, y las dos costaron un fallo:
		//
		//  1. Medir contra el PRIMER punto no vale: si el GPS tiembla 22 m a cada
		//     lado, dos fixes consecutivos distan 60 m aunque ninguno se aleje
		//     31 m del centro real, y la parada no llegaría a formarse nunca. Al
		//     segundo punto se le exige la mitad —el radio de una pareja es la
		//     mitad de lo que las separa—; admitirlo sin condición fabricaba
		//     paradas falsas entre dos fixes lejanos cuando el aparato muestrea
		//     cada varios minutos.
		//
		//  2. Recalcular el radio del grupo entero en cada paso es O(n²). Con los
		//     49 000 puntos de un fichero real de GPSLogger —muestrea cada
		//     segundo— eso eran casi treinta segundos por día. El centro se lleva
		//     incremental y queda lineal.
		sumLat, sumLon := puntos[i].Lat, puntos[i].Lon
		n := 1.0
		j := i + 1
		for j < len(puntos) {
			centro := Coord{Lat: sumLat / n, Lon: sumLon / n}
			limite := cfg.ParadaRadioM
			if n == 1 {
				limite = 2 * cfg.ParadaRadioM
			}
			if DistanciaM(centro, puntos[j].coord()) > limite {
				break
			}
			sumLat += puntos[j].Lat
			sumLon += puntos[j].Lon
			n++
			j++
		}

		grupo := puntos[i:j]
		if len(grupo) > 1 && grupo[0].Ts != nil && grupo[len(grupo)-1].Ts != nil {
			inicio, fin := *grupo[0].Ts, *grupo[len(grupo)-1].Ts
			dur := int(fin.Sub(inicio).Minutes())
			if dur >= cfg.ParadaMinutos {
				coords := aCoords(grupo)
				c, _ := Centroide(coords)
				paradas = append(paradas, Parada{
					Inicio:      inicio,
					Fin:         fin,
					DuracionMin: dur,
					Lat:         c.Lat,
					Lon:         c.Lon,
					RadioM:      RadioDispersionM(coords),
					Seq:         seq,
				})
				seq++
			}
		}

		// Se avanza SIEMPRE hasta donde llegó el grupo, haya salido parada o no.
		//
		// Volver a empezar en i+1 cuando el grupo era demasiado corto parece más
		// cuidadoso, pero es lo que hacía el cálculo cuadrático: con los 25 000
		// puntos de una jornada real, diecisiete segundos por día. Y no se pierde
		// nada: si todo el grupo cabía en el radio y aun así duró menos de lo
		// exigido, cualquier trozo suyo dura todavía menos.
		i = j
	}

	return paradas
}

func aCoords(puntos []PuntoEvaluado) []Coord {
	out := make([]Coord, len(puntos))
	for i, p := range puntos {
		out[i] = p.coord()
	}
	return out
}

// distanciaNeta suma el recorrido descontando el temblor del GPS.
//
// La versión evidente —descartar cada tramo de menos de PasoMinimoM— parece
// razonable y es falsa: los loggers reales muestrean cada segundo, así que dos
// fixes consecutivos distan tres o cuatro metros AUNQUE la persona vaya en
// coche. Filtrando paso a paso se descarta el día entero y el recorrido sale de
// cero kilómetros.
//
// Lo correcto es medir contra un ANCLA: se avanza mientras el punto siga cerca
// del ancla —eso es temblor— y solo cuando se aleja de verdad se suma el salto y
// el ancla pasa a ser ese punto. Así el resultado no depende de cada cuánto
// muestree el aparato, que es justo lo que no debe cambiar la cuenta.
func distanciaNeta(puntos []PuntoEvaluado, pasoMinimoM float64) float64 {
	if len(puntos) < 2 {
		return 0
	}
	var metros float64
	ancla := puntos[0]
	for _, p := range puntos[1:] {
		if d := DistanciaM(ancla.coord(), p.coord()); d >= pasoMinimoM {
			metros += d
			ancla = p
		}
	}
	return metros
}

// cobertura mide qué parte de la jornada tuvo señal.
//
// Es la métrica honesta frente a "kilómetros recorridos": alguien puede recorrer
// 40 km en una hora y apagar el GPS el resto del día.
func cobertura(puntos []PuntoEvaluado, cfg Config) (float64, int) {
	ventana := float64(HoraAMinutos(cfg.JornadaFin) - HoraAMinutos(cfg.JornadaInicio))
	if len(puntos) < 2 || ventana <= 0 {
		return 0, 0
	}
	var cubierto float64
	huecos := 0
	for i := 1; i < len(puntos); i++ {
		min := puntos[i].Ts.Sub(*puntos[i-1].Ts).Minutes()
		if min <= cfg.CoberturaGapMin {
			cubierto += min
		}
		if min >= float64(cfg.HuecoMinutos) {
			huecos++
		}
	}
	return math.Min(100, math.Round(cubierto/ventana*1000)/10), huecos
}

// CalcularDia emite el veredicto del día.
func CalcularDia(entrada []PuntoEntrada, cfg Config) ResultadoDia {
	puntos := EvaluarPuntos(entrada, cfg)
	res := ResultadoDia{Estado: DiaSinFichero, Puntos: puntos, Banderas: []string{}}

	if len(puntos) == 0 {
		return res
	}

	// Hay coordenadas pero ninguna hora utilizable: se puede pintar el recorrido,
	// pero no hay jornada, ni paradas, ni cobertura. No es un día bueno ni una
	// ausencia: es su propio caso, y sale en amarillo en el calendario.
	conHora := []PuntoEvaluado{}
	for _, p := range puntos {
		if p.Ts != nil && p.Quality != CalidadSinHora {
			conHora = append(conHora, p)
		}
	}
	if len(conHora) == 0 {
		coords := aCoords(puntos)
		c, _ := Centroide(coords)
		res.Estado = DiaSinFecha
		res.Centroide = &c
		res.RadioDispersion = math.Round(RadioDispersionM(coords))
		res.Banderas = append(res.Banderas, BanderaSinHoras)
		return res
	}

	jornada := puntosDeJornada(puntos, cfg)
	if len(jornada) == 0 {
		coords := aCoords(conHora)
		c, _ := Centroide(coords)
		res.Estado = DiaSinMovimiento
		res.PrimerFix = conHora[0].Ts
		res.UltimoFix = conHora[len(conHora)-1].Ts
		res.Centroide = &c
		res.RadioDispersion = math.Round(RadioDispersionM(coords))
		res.Banderas = append(res.Banderas, BanderaSinDatosJornada)
		return res
	}

	kmNetos := math.Round(distanciaNeta(jornada, cfg.PasoMinimoM)/1000*100) / 100

	paradas := DetectarParadas(jornada, cfg)
	minParado := 0
	for _, p := range paradas {
		minParado += p.DuracionMin
	}

	primerFix := *jornada[0].Ts
	ultimoFix := *jornada[len(jornada)-1].Ts
	spanMin := int(ultimoFix.Sub(primerFix).Minutes())
	minMovimiento := spanMin - minParado
	if minMovimiento < 0 {
		minMovimiento = 0
	}
	cob, huecos := cobertura(jornada, cfg)
	coords := aCoords(jornada)
	radio := RadioDispersionM(coords)
	centro, _ := Centroide(coords)

	banderas := []string{}
	if minutosLocales(primerFix, cfg.Zona) > HoraAMinutos(cfg.JornadaInicio)+cfg.ToleranciaEntradaMin {
		banderas = append(banderas, BanderaEntradaTardia)
	}
	if minutosLocales(ultimoFix, cfg.Zona) < HoraAMinutos(cfg.JornadaFin)-cfg.ToleranciaSalidaMin {
		banderas = append(banderas, BanderaSalidaTemprana)
	}
	if huecos > 0 {
		banderas = append(banderas, BanderaHuecoLargo)
	}
	if cob < cfg.CoberturaMinima {
		banderas = append(banderas, BanderaPocaCobertura)
	}

	// "Sin movimiento" se decide por el RADIO DE DISPERSIÓN, no por los kilómetros.
	//
	// Los kilómetros son justo la métrica que el ruido del GPS corrompe: con fixes
	// que bailan 20 m, un teléfono inmóvil acumula varios kilómetros en una
	// jornada y se cuela como día trabajado. El radio no se corrompe: si en siete
	// horas los puntos nunca se alejaron 300 m de su centro, esa persona no hizo
	// una ruta, por muchos kilómetros de temblor que sumen.
	//
	// La segunda condición es de honestidad, no de detección: hace falta una
	// jornada lo bastante larga para afirmarlo. Con tres fixes de las nueve de la
	// mañana lo que hay es poca cobertura, no una acusación de inmovilidad.
	estado := DiaOK
	switch {
	case radio < cfg.SinMovRadioM && spanMin >= cfg.SinMovSpanMin:
		estado = DiaSinMovimiento
		banderas = append(banderas, BanderaSinMovimiento)
	case kmNetos < cfg.EscasoKmNetos:
		estado = DiaMovimientoEscaso
		banderas = append(banderas, BanderaMovimientoEscaso)
	}

	res.Estado = estado
	res.PuntosValidos = len(jornada)
	res.PrimerFix = &primerFix
	res.UltimoFix = &ultimoFix
	res.KmNetos = kmNetos
	res.MinMovimiento = minMovimiento
	res.MinParado = minParado
	res.Cobertura = cob
	res.Huecos = huecos
	res.RadioDispersion = math.Round(radio)
	res.Centroide = &centro
	res.Paradas = paradas
	res.Banderas = banderas
	return res
}
