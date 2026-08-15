package metrics

import "math"

// Geometry on a sphere. No dependencies: it is four formulas, and this way the
// metrics engine is tested end to end without a database or PostGIS.

const radioTierraM = 6371008.8

// Coord is a geographic coordinate.
type Coord struct {
	Lat float64
	Lon float64
}

func rad(g float64) float64 { return g * math.Pi / 180 }

// DistanceM returns the metres between two coordinates (haversine).
func DistanceM(a, b Coord) float64 {
	dLat := rad(b.Lat - a.Lat)
	dLon := rad(b.Lon - a.Lon)
	h := math.Pow(math.Sin(dLat/2), 2) +
		math.Cos(rad(a.Lat))*math.Cos(rad(b.Lat))*math.Pow(math.Sin(dLon/2), 2)
	return 2 * radioTierraM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Centroid is the centre of mass of a cloud of points.
func Centroid(puntos []Coord) (Coord, bool) {
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

// SpreadRadiusM is the greatest distance to the centroid, in metres.
//
// It is the signal that tells "sat still all day" apart from "worked the route":
// a phone that never moves piles up kilometres of pure GPS noise, but its cloud
// of points never strays more than a few dozen metres from the centre.
func SpreadRadiusM(puntos []Coord) float64 {
	c, ok := Centroid(puntos)
	if !ok {
		return 0
	}
	var max float64
	for _, p := range puntos {
		if d := DistanceM(c, p); d > max {
			max = d
		}
	}
	return max
}

// SpeedKmh between two points `seconds` apart.
func SpeedKmh(a, b Coord, segundos float64) float64 {
	if segundos <= 0 {
		return 0
	}
	return DistanceM(a, b) / segundos * 3.6
}
