package api

import "strings"

// Cruzar la sucursal de Accesos con la de aquí, cuando los nombres no coinciden.
//
// Las de aquí nacieron del nombre de la cuenta de Drive y las de Accesos se dieron de
// alta a mano, así que la misma sucursal está escrita de dos formas y sólo coincide por
// casualidad. La clave exacta cuadraba en cinco de diez, y las otras cinco dejaban a su
// gente SIN SUCURSAL — y un administrador sin sucursal no ve absolutamente nada, sin que
// nada le diga por qué. Pasó con Daniel, administrador de La Habana:
//
//	aquí «Habana» (habana)                   Accesos «La Habana»       (lahabana)
//	aquí «Sancti Spíritus» (santispiritus)   Accesos «Sancti Spíritus» (sanctispiritus)
//	aquí «Santiago» (santiago)               Accesos «Santiago de Cuba» (santiagodecuba)
//
// Se prueban tres reglas, de la más fiel a la más permisiva, y siempre exigiendo UNA sola
// candidata: darle a alguien la sucursal de otro es peor que dejarlo sin ninguna, porque
// se ve trabajo ajeno sin saberlo.
//
//  1. la clave exacta;
//  2. que una contenga a la otra («lahabana» contiene «habana»);
//  3. una diferencia de una o dos letras («santispiritus» vs «sanctispiritus»).

// distancia de edición, con tope: en cuanto pasa de `max` no hace falta seguir contando.
func distanciaHasta(a, b string, max int) int {
	if a == b {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > max {
		return max + 1
	}

	anterior := make([]int, len(a)+1)
	actual := make([]int, len(a)+1)
	for i := range anterior {
		anterior[i] = i
	}

	for j := 1; j <= len(b); j++ {
		actual[0] = j
		mejor := actual[0]
		for i := 1; i <= len(a); i++ {
			coste := 1
			if a[i-1] == b[j-1] {
				coste = 0
			}
			actual[i] = min3(actual[i-1]+1, anterior[i]+1, anterior[i-1]+coste)
			if actual[i] < mejor {
				mejor = actual[i]
			}
		}
		if mejor > max {
			return max + 1
		}
		copy(anterior, actual)
	}
	return anterior[len(a)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// SucursalCandidata: lo mínimo para poder cruzar.
type SucursalCandidata struct {
	ID     string
	Clave  string
	Codigo string
}

// EmparejarSucursal devuelve el id de la sucursal de aquí que corresponde al nombre (y
// código, si lo hay) de Accesos. Cadena vacía cuando no hay UNA sola clara.
func EmparejarSucursal(nombreAuth, codigoAuth string, candidatas []SucursalCandidata) string {
	clave := claveDeSucursal(nombreAuth)
	if clave == "" {
		return ""
	}

	// 1. Exacta, por clave o por código: es la que no se discute.
	codigo := strings.ToUpper(strings.TrimSpace(codigoAuth))
	for _, c := range candidatas {
		if c.Clave == clave {
			return c.ID
		}
		if codigo != "" && strings.EqualFold(strings.TrimSpace(c.Codigo), codigo) {
			return c.ID
		}
	}

	// 2. Que una contenga a la otra, y sólo una.
	unica := func(elegidas map[string]bool) string {
		if len(elegidas) != 1 {
			return ""
		}
		for id := range elegidas {
			return id
		}
		return ""
	}

	contiene := map[string]bool{}
	for _, c := range candidatas {
		// Claves muy cortas encajan en cualquier parte: no valen para esto.
		if len(c.Clave) < 5 {
			continue
		}
		if strings.Contains(clave, c.Clave) || strings.Contains(c.Clave, clave) {
			contiene[c.ID] = true
		}
	}
	if id := unica(contiene); id != "" {
		return id
	}

	// 3. Una o dos letras de diferencia, y sólo una candidata.
	cerca := map[string]bool{}
	for _, c := range candidatas {
		if len(c.Clave) < 5 {
			continue
		}
		if distanciaHasta(clave, c.Clave, 2) <= 2 {
			cerca[c.ID] = true
		}
	}
	return unica(cerca)
}
