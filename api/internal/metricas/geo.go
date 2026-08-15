package metricas

import "math"

// Geometría sobre la esfera. Sin dependencias: son cuatro fórmulas, y así el
// motor de métricas se prueba entero sin base de datos ni PostGIS.

const radioTierraM = 6371008.8

// Coord es una coordenada geográfica.
type Coord struct {
	Lat float64
	Lon float64
}

func rad(g float64) float64 { return g * math.Pi / 180 }

// DistanciaM devuelve los metros entre dos coordenadas (haversine).
func DistanciaM(a, b Coord) float64 {
	dLat := rad(b.Lat - a.Lat)
	dLon := rad(b.Lon - a.Lon)
	h := math.Pow(math.Sin(dLat/2), 2) +
		math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Pow(math.Sin(dLon/2), 2)
	return 2 * radioTierraM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Centroide es el centro de masa de una nube de puntos.
func Centroide(puntos []Coord) (Coord, bool) {
	if len(puntos) == 0 {
		return Coord{}, false
	}
	var lat, lon float64
	for _, p := range puntos {
		lat += p.Lat
		lon += p.Lon
	}
	n := float64(len(puntos))
	return Coord{Lat: lat / n, Lon: lon / n}, true
}

// RadioDispersionM es la distancia máxima al centroide, en metros.
//
// Es la señal que distingue "se pasó el día quieto" de "recorrió su ruta": un
// teléfono inmóvil acumula kilómetros de puro ruido del GPS, pero su nube de
// puntos nunca se aleja más de unas decenas de metros del centro.
func RadioDispersionM(puntos []Coord) float64 {
	c, ok := Centroide(puntos)
	if !ok {
		return 0
	}
	var max float64
	for _, p := range puntos {
		if d := DistanciaM(c, p); d > max {
			max = d
		}
	}
	return max
}

// VelocidadKmh entre dos puntos separados por `segundos`.
func VelocidadKmh(a, b Coord, segundos float64) float64 {
	if segundos <= 0 {
		return 0
	}
	return DistanciaM(a, b) / segundos * 3.6
}
