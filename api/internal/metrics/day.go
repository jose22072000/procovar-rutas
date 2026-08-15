// Package metrics decides how a seller's day went.
//
// Everything the compliance calendar paints comes from here: kilometres, stops,
// time coverage and — the thing actually worth knowing — whether the day is OK,
// SIN_FECHA or SIN_MOVIMIENTO.
//
// ComputeDay is a pure function: points and a configuration go in, the verdict
// comes out. That way it is tested end to end without a database, and the
// thresholds can be retuned once the real GPX files arrive without touching
// ingest.
package metrics

import (
	"math"
	"sort"
	"time"
)

// DayStatus is the verdict painted in each calendar cell.
type DayStatus string

const (
	DayOK               DayStatus = "OK"
	DayNoFile       DayStatus = "SIN_FICHERO"
	DayNoDate         DayStatus = "SIN_FECHA"
	DayNoMovement    DayStatus = "SIN_MOVIMIENTO"
	DayLowMovement DayStatus = "MOVIMIENTO_ESCASO"
	DayNotWorking      DayStatus = "NO_LABORABLE"
)

// Quality says why a point does not count. It is flagged, NOT deleted: if the
// threshold turns out to be wrong tomorrow, the day is recomputed without
// downloading anything from Drive again.
type Quality string

const (
	QualityOK        Quality = "OK"
	QualityJump     Quality = "SALTO"
	QualityImprecise Quality = "IMPRECISO"
	QualityDuplicate Quality = "DUPLICADO"
	QualityNoTime   Quality = "SIN_HORA"
)

// Flags attached to a day.
const (
	FlagLateStart    = "entrada_tardia"
	FlagEarlyEnd   = "salida_temprana"
	FlagLongGap       = "hueco_largo"
	FlagLowCoverage    = "poca_cobertura"
	FlagNoMovement    = "sin_movimiento"
	FlagLowMovement = "movimiento_escaso"
	FlagNoTimes         = "sin_horas"
	FlagNoWorkdayData  = "sin_datos_en_jornada"
)

// InputPoint is a fix exactly as the parser produces it.
type InputPoint struct {
	Lat      float64
	Lon      float64
	Ts       *time.Time
	Accuracy *float64
	Seq      int
}

func (p InputPoint) coord() Coord { return Coord{Lat: p.Lat, Lon: p.Lon} }

// ScoredPoint is a fix that has already been judged.
type ScoredPoint struct {
	InputPoint
	Quality Quality
	// Speed in km/h relative to the last good point.
	Speed float64
}

// Config holds the thresholds, configurable per branch: a seller working the
// city is not the same as one covering three municipalities.
type Config struct {
	Zone         *time.Location
	WorkdayStart string // "09:00"
	WorkdayEnd   string // "16:00"
	StopRadiusM  float64
	StopMinutes  int
	// MinStepM: below this distance a leg is noise and does not add kilometres.
	MinStepM      float64
	NoMoveRadiusM float64
	// NoMoveSpanMin: the shortest workday span that allows claiming nobody moved.
	NoMoveSpanMin     int
	LowNetKm          float64
	MaxSpeedKmh       float64
	MaxAccuracyM      float64
	GapMinutes        int
	CoverageGapMin    float64
	MinCoverage       float64
	EntryToleranceMin int
	ExitToleranceMin  int
}

// DefaultConfig holds the starting values. They will be tuned with real data.
func DefaultConfig() Config {
	zona, err := time.LoadLocation("America/Havana")
	if err != nil {
		// With no time zone database, a fixed UTC-4. Worse (it does not follow
		// daylight saving) but better than blowing up.
		zona = time.FixedZone("CUT", -4*3600)
	}
	return Config{
		Zone:              zona,
		WorkdayStart:      "09:00",
		WorkdayEnd:        "16:00",
		StopRadiusM:       60,
		StopMinutes:       5,
		MinStepM:          25,
		NoMoveRadiusM:     300,
		NoMoveSpanMin:     120,
		LowNetKm:          5,
		MaxSpeedKmh:       150,
		MaxAccuracyM:      100,
		GapMinutes:        30,
		CoverageGapMin:    5,
		MinCoverage:       70,
		EntryToleranceMin: 15,
		ExitToleranceMin:  15,
	}
}

// Stop is a span during which the seller did not move from the spot.
type Stop struct {
	Start       time.Time
	End         time.Time
	DurationMin int
	Lat         float64
	Lon         float64
	RadiusM     float64
	Seq         int
}

// DayResult is what gets stored in track_day.
type DayResult struct {
	Status      DayStatus
	Points      []ScoredPoint
	ValidPoints int
	FirstFix    *time.Time
	LastFix     *time.Time
	NetKm       float64
	MinMovement int
	MinStopped  int
	Coverage    float64
	Gaps        int
	SpreadM     float64
	Centroid    *Coord
	Stops       []Stop
	Flags       []string
}

// TimeToMinutes turns "09:00" into 540.
func TimeToMinutes(hhmm string) int {
	var h, m int
	if len(hhmm) >= 5 {
		h = int(hhmm[0]-'0')*10 + int(hhmm[1]-'0')
		m = int(hhmm[3]-'0')*10 + int(hhmm[4]-'0')
	}
	return h*60 + m
}

// localMinutes returns minutes since midnight in the branch's local time.
//
// Always through time.Location, never by adding hours by hand: Cuba observes
// daylight saving, and the March and November reports would come out shifted
// during exactly the weeks nobody thinks to double-check them.
func localMinutes(t time.Time, zona *time.Location) int {
	l := t.In(zona)
	return l.Hour()*60 + l.Minute()
}

// ScorePoints flags the bad points without deleting them.
func ScorePoints(entrada []InputPoint, cfg Config) []ScoredPoint {
	ordenados := make([]InputPoint, len(entrada))
	copy(ordenados, entrada)
	sort.SliceStable(ordenados, func(i, j int) bool {
		a, b := ordenados[i], ordenados[j]
		if a.Ts != nil && b.Ts != nil {
			return a.Ts.Before(*b.Ts)
		}
		return a.Seq < b.Seq
	})

	out := make([]ScoredPoint, 0, len(ordenados))
	vistos := map[int64]bool{}
	var anterior *ScoredPoint

	for _, p := range ordenados {
		ev := ScoredPoint{InputPoint: p, Quality: QualityOK}

		switch {
		case p.Ts == nil:
			ev.Quality = QualityNoTime
		case vistos[p.Ts.UnixNano()]:
			ev.Quality = QualityDuplicate
		case p.Accuracy != nil && *p.Accuracy > cfg.MaxAccuracyM:
			ev.Quality = QualityImprecise
		case anterior != nil && anterior.Ts != nil:
			seg := p.Ts.Sub(*anterior.Ts).Seconds()
			ev.Speed = VelocidadKmh(anterior.coord(), p.coord(), seg)
			// An impossible jump is a bounced fix, not a seller on a plane. It is
			// flagged and NOT used as a reference: chaining from it would drag the
			// error through the rest of the route.
			if ev.Speed > cfg.MaxSpeedKmh {
				ev.Quality = QualityJump
			}
		}

		if p.Ts != nil {
			vistos[p.Ts.UnixNano()] = true
		}
		out = append(out, ev)
		if ev.Quality == QualityOK {
			ultimo := out[len(out)-1]
			anterior = &ultimo
		}
	}

	return out
}

// workdayPoints keeps the usable points inside working hours.
func workdayPoints(puntos []ScoredPoint, cfg Config) []ScoredPoint {
	ini := TimeToMinutes(cfg.WorkdayStart)
	fin := TimeToMinutes(cfg.WorkdayEnd)
	out := []ScoredPoint{}
	for _, p := range puntos {
		if p.Quality != QualityOK || p.Ts == nil {
			continue
		}
		m := localMinutes(*p.Ts, cfg.Zone)
		if m >= ini && m <= fin {
			out = append(out, p)
		}
	}
	return out
}

// DetectStops groups consecutive points that stay near the group's centre for at
// least StopMinutes.
//
// The default radius is 60 m and not 10: in town, a stationary GPS "dances"
// 20–30 m between buildings. With a small radius every visit would split into
// five false stops and the report would be unreadable.
func DetectStops(puntos []ScoredPoint, cfg Config) []Stop {
	paradas := []Stop{}
	i := 0
	seq := 0

	for i < len(puntos) {
		// The group grows while the next point stays near the group's CENTRE, with
		// the centre updated as it goes.
		//
		// Two traps, and both cost a bug:
		//
		//  1. Medir contra el PRIMER punto no vale: si el GPS tiembla 22 m a cada
		//     lado, dos fixes consecutivos distan 60 m aunque ninguno se aleje
		//     31 m del centro real, y la parada no llegaría a formarse nunca. Al
		//     segundo punto se le exige la mitad —el radio de una pareja es la
		//     mitad de lo que las separa—; admitirlo sin condición fabricaba
		//     paradas falsas entre dos fixes lejanos cuando el aparato muestrea
		//     cada varios minutos.
		//
		//  2. Recomputing the whole group's radius at every step is O(n²). With the
		//     49,000 points of a real GPSLogger file — it samples once a second —
		//     that came to nearly thirty seconds per day. The centre is kept
		//     incrementally, which makes it linear.
		sumLat, sumLon := puntos[i].Lat, puntos[i].Lon
		n := 1.0
		j := i + 1
		for j < len(puntos) {
			centro := Coord{Lat: sumLat / n, Lon: sumLon / n}
			limite := cfg.StopRadiusM
			if n == 1 {
				limite = 2 * cfg.StopRadiusM
			}
			if DistanceM(centro, puntos[j].coord()) > limite {
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
			if dur >= cfg.StopMinutes {
				coords := aCoords(grupo)
				c, _ := Centroid(coords)
				paradas = append(paradas, Stop{
					Start:       inicio,
					End:         fin,
					DurationMin: dur,
					Lat:         c.Lat,
					Lon:         c.Lon,
					RadiusM:     SpreadRadiusM(coords),
					Seq:         seq,
				})
				seq++
			}
		}

		// It ALWAYS advances to where the group ended, stop or no stop.
		//
		// Restarting at i+1 when the group was too short looks more careful, but
		// that is what made the computation quadratic: with the 25,000 points of a
		// real workday, seventeen seconds per day. And nothing is lost: if the whole
		// group fitted inside the radius and still lasted less than
		// exigido, cualquier trozo suyo dura todavía menos.
		i = j
	}

	return paradas
}

func aCoords(puntos []ScoredPoint) []Coord {
	out := make([]Coord, len(puntos))
	for i, p := range puntos {
		out[i] = p.coord()
	}
	return out
}

// netDistance adds up the route while discounting GPS wobble.
//
// The obvious version — drop every leg shorter than MinStepM — looks reasonable
// and is wrong: real loggers sample once a second, so two consecutive fixes are
// three or four metres apart EVEN IF the person is driving. Filtering leg by leg
// throws away the entire day and the route comes out as
// cero kilómetros.
//
// The right way is to measure against an ANCHOR: keep going while the point
// stays near the anchor — that is wobble — and only when it genuinely moves away
// add the jump and make that point the new anchor. The result then does not
// depend on how often the device samples, which is exactly what must not change
// the total.
func netDistance(puntos []ScoredPoint, pasoMinimoM float64) float64 {
	if len(puntos) < 2 {
		return 0
	}
	var metros float64
	ancla := puntos[0]
	for _, p := range puntos[1:] {
		if d := DistanceM(ancla.coord(), p.coord()); d >= pasoMinimoM {
			metros += d
			ancla = p
		}
	}
	return metros
}

// coverage measures how much of the workday had signal.
//
// It is the honest metric next to "kilometres travelled": someone can cover
// 40 km in an hour and switch the GPS off for the rest of the day.
func cobertura(puntos []ScoredPoint, cfg Config) (float64, int) {
	ventana := float64(TimeToMinutes(cfg.WorkdayEnd) - TimeToMinutes(cfg.WorkdayStart))
	if len(puntos) < 2 || ventana <= 0 {
		return 0, 0
	}
	var cubierto float64
	huecos := 0
	for i := 1; i < len(puntos); i++ {
		min := puntos[i].Ts.Sub(*puntos[i-1].Ts).Minutes()
		if min <= cfg.CoverageGapMin {
			cubierto += min
		}
		if min >= float64(cfg.GapMinutes) {
			huecos++
		}
	}
	return math.Min(100, math.Round(cubierto/ventana*1000)/10), huecos
}

// ComputeDay issues the day's verdict.
func ComputeDay(entrada []InputPoint, cfg Config) DayResult {
	puntos := ScorePoints(entrada, cfg)
	res := DayResult{Status: DayNoFile, Points: puntos, Flags: []string{}}

	if len(puntos) == 0 {
		return res
	}

	// There are coordinates but no usable timestamps: the route can be drawn, but
	// there is no workday, no stops and no coverage. It is neither a good day nor
	// an absence: it is its own case, and shows up amber on the calendar.
	conHora := []ScoredPoint{}
	for _, p := range puntos {
		if p.Ts != nil && p.Quality != QualityNoTime {
			conHora = append(conHora, p)
		}
	}
	if len(conHora) == 0 {
		coords := aCoords(puntos)
		c, _ := Centroid(coords)
		res.Status = DayNoDate
		res.Centroid = &c
		res.SpreadM = math.Round(SpreadRadiusM(coords))
		res.Flags = append(res.Flags, FlagNoTimes)
		return res
	}

	jornada := workdayPoints(puntos, cfg)
	if len(jornada) == 0 {
		coords := aCoords(conHora)
		c, _ := Centroid(coords)
		res.Status = DayNoMovement
		res.FirstFix = conHora[0].Ts
		res.LastFix = conHora[len(conHora)-1].Ts
		res.Centroid = &c
		res.SpreadM = math.Round(SpreadRadiusM(coords))
		res.Flags = append(res.Flags, FlagNoWorkdayData)
		return res
	}

	kmNetos := math.Round(netDistance(jornada, cfg.MinStepM)/1000*100) / 100

	paradas := DetectStops(jornada, cfg)
	minParado := 0
	for _, p := range paradas {
		minParado += p.DurationMin
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
	radio := SpreadRadiusM(coords)
	centro, _ := Centroid(coords)

	banderas := []string{}
	if localMinutes(primerFix, cfg.Zone) > TimeToMinutes(cfg.WorkdayStart)+cfg.EntryToleranceMin {
		banderas = append(banderas, FlagLateStart)
	}
	if localMinutes(ultimoFix, cfg.Zone) < TimeToMinutes(cfg.WorkdayEnd)-cfg.ExitToleranceMin {
		banderas = append(banderas, FlagEarlyEnd)
	}
	if huecos > 0 {
		banderas = append(banderas, FlagLongGap)
	}
	if cob < cfg.MinCoverage {
		banderas = append(banderas, FlagLowCoverage)
	}

	// "No movement" is decided by the SPREAD RADIUS, not by kilometres.
	//
	// Los kilómetros son justo la métrica que el ruido del GPS corrompe: con fixes
	// que bailan 20 m, un teléfono inmóvil acumula varios kilómetros en una
	// jornada y se cuela como día trabajado. El radio no se corrompe: si en siete
	// horas los puntos nunca se alejaron 300 m de su centro, esa persona no hizo
	// una ruta, por muchos kilómetros de temblor que sumen.
	//
	// The second condition is about honesty, not detection: the workday has to be
	// long enough to make the claim. With three fixes from nine in the morning what
	// you have is poor coverage, not an accusation of sitting still.
	estado := DayOK
	switch {
	case radio < cfg.NoMoveRadiusM && spanMin >= cfg.NoMoveSpanMin:
		estado = DayNoMovement
		banderas = append(banderas, FlagNoMovement)
	case kmNetos < cfg.LowNetKm:
		estado = DayLowMovement
		banderas = append(banderas, FlagLowMovement)
	}

	res.Status = estado
	res.ValidPoints = len(jornada)
	res.FirstFix = &primerFix
	res.LastFix = &ultimoFix
	res.NetKm = kmNetos
	res.MinMovement = minMovimiento
	res.MinStopped = minParado
	res.Coverage = cob
	res.Gaps = huecos
	res.SpreadM = math.Round(radio)
	res.Centroid = &centro
	res.Stops = paradas
	res.Flags = banderas
	return res
}
