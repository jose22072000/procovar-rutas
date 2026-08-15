// Package calendario resuelve qué días cuentan.
//
// La jornada de la empresa es de lunes a viernes, así que un sábado sin fichero
// no es una falta. Los feriados tampoco: sin tenerlos en cuenta, un 1 de mayo
// aparecería como ausencia de toda la plantilla y el panel se volvería ruido.
package calendario

import "time"

// LaborablesPorDefecto: lunes (1) a viernes (5), en numeración ISO.
var LaborablesPorDefecto = []int{1, 2, 3, 4, 5}

// EsLaborable dice si una fecha cuenta para el control.
func EsLaborable(fecha time.Time, laborables []int, feriados map[string]bool) bool {
	if feriados[fecha.Format("2006-01-02")] {
		return false
	}
	if laborables == nil {
		laborables = LaborablesPorDefecto
	}
	iso := int(fecha.Weekday())
	if iso == 0 {
		iso = 7 // domingo
	}
	for _, d := range laborables {
		if d == iso {
			return true
		}
	}
	return false
}

// DiasLaborables lista los días de trabajo entre dos fechas, ambas incluidas.
func DiasLaborables(desde, hasta time.Time, feriados map[string]bool) []string {
	out := []string{}
	for d := desde; !d.After(hasta); d = d.AddDate(0, 0, 1) {
		if EsLaborable(d, LaborablesPorDefecto, feriados) {
			out = append(out, d.Format("2006-01-02"))
		}
	}
	return out
}

// SemanaLaboral devuelve el lunes a viernes de la semana que contiene la fecha.
//
// Cinco días, no siete: el reporte y la vista semanal son de la jornada laboral.
// Un sábado con datos se guarda igual, pero no cuenta como incumplimiento.
func SemanaLaboral(fecha time.Time) []time.Time {
	iso := int(fecha.Weekday())
	if iso == 0 {
		iso = 7
	}
	lunes := fecha.AddDate(0, 0, -(iso - 1))
	lunes = time.Date(lunes.Year(), lunes.Month(), lunes.Day(), 0, 0, 0, 0, fecha.Location())

	semana := make([]time.Time, 5)
	for i := range semana {
		semana[i] = lunes.AddDate(0, 0, i)
	}
	return semana
}
