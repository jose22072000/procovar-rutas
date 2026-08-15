// Package alcance decide quién puede ver los recorridos de quién.
//
// Todas las consultas del sistema pasan obligatoriamente por aquí. Un filtro
// repetido a mano en doce manejadores es un filtro que alguien olvidará en el
// trece — y ese olvido, en esta aplicación, significa que un supervisor ve el
// recorrido de otro equipo, o el suyo propio.
//
// Dos reglas que no son negociables:
//
//  1. Nadie ve su propio recorrido. Es una aplicación de supervisión: quien mira
//     no es sujeto de la mirada.
//  2. El alcance del supervisor se evalúa contra la FECHA CONSULTADA, no contra
//     hoy. Si en octubre pides la semana de agosto, ves a los vendedores que
//     supervisabas en agosto. Por eso la supervisión lleva vigencias.
package alcance

import (
	"errors"
	"sort"
	"time"
)

// Rol es el papel de la persona en Procovar. Sale de procovar-auth.
type Rol string

const (
	RolSuperAdmin Rol = "super_admin"
	RolAdmin      Rol = "admin"
	RolGerente    Rol = "gerente"
	RolSupervisor Rol = "supervisor"
	RolGestor     Rol = "gestor"
)

// ErrSinAcceso lo devuelve el gestor, que no entra a esta aplicación. Es un
// error y no un resultado vacío para que la respuesta sea 403 y no una pantalla
// en blanco que parezca un fallo del sistema.
var ErrSinAcceso = errors.New("este rol no tiene acceso a los recorridos")

// Sesion es la identidad ya verificada contra procovar-auth.
type Sesion struct {
	AuthUserID string
	// TrabajadorID es su ficha local, si la tiene. Es la que se excluye.
	TrabajadorID string
	SucursalID   string
	Rol          Rol
}

// Vigencia es "esta persona supervisaba a esta otra entre estas fechas".
type Vigencia struct {
	GestorID     string
	SupervisorID string
	Desde        time.Time
	// Hasta nil = sigue vigente.
	Hasta *time.Time
}

// Filtro es la cláusula que toda consulta debe aplicar.
type Filtro struct {
	// SucursalID vacío = sin restricción de sucursal.
	SucursalID string
	// TrabajadoresIn, si no es nil, limita a esos vendedores.
	TrabajadoresIn []string
	// TrabajadorNot excluye a una persona (siempre, la que consulta).
	TrabajadorNot string
	// Vacio = este rol no puede ver nada; la consulta debe devolver cero filas.
	Vacio bool
}

// GestoresVigentes son los vendedores a cargo de un supervisor en una fecha.
func GestoresVigentes(vigencias []Vigencia, supervisorID string, fecha time.Time) []string {
	ids := []string{}
	visto := map[string]bool{}
	for _, v := range vigencias {
		if v.SupervisorID != supervisorID || visto[v.GestorID] {
			continue
		}
		if v.Desde.After(fecha) {
			continue
		}
		if v.Hasta != nil && v.Hasta.Before(fecha) {
			continue
		}
		visto[v.GestorID] = true
		ids = append(ids, v.GestorID)
	}
	sort.Strings(ids)
	return ids
}

// Calcular devuelve el filtro de alcance.
//
// `fecha` es la del dato consultado, no la de hoy. `vigencias` solo hace falta
// para el rol supervisor; los demás no la miran.
func Calcular(s Sesion, fecha time.Time, vigencias []Vigencia) (Filtro, error) {
	switch s.Rol {
	case RolSuperAdmin:
		// Todo. Se le excluye a sí mismo por si además es vendedor en alguna
		// sucursal.
		return Filtro{TrabajadorNot: s.TrabajadorID}, nil

	case RolAdmin, RolGerente:
		if s.SucursalID == "" {
			return Filtro{Vacio: true}, nil
		}
		return Filtro{SucursalID: s.SucursalID, TrabajadorNot: s.TrabajadorID}, nil

	case RolSupervisor:
		if s.TrabajadorID == "" {
			return Filtro{Vacio: true}, nil
		}
		suyos := []string{}
		for _, id := range GestoresVigentes(vigencias, s.TrabajadorID, fecha) {
			if id != s.TrabajadorID {
				suyos = append(suyos, id)
			}
		}
		// Sin equipo vigente esa fecha no ve nada. Devolver "todo" ante la duda
		// sería el fallo abierto clásico.
		if len(suyos) == 0 {
			return Filtro{Vacio: true}, nil
		}
		return Filtro{TrabajadoresIn: suyos, TrabajadorNot: s.TrabajadorID}, nil

	case RolGestor:
		return Filtro{Vacio: true}, ErrSinAcceso

	default:
		return Filtro{Vacio: true}, nil
	}
}

// PuedeAdministrar: fuentes de Drive, alias y umbrales.
func PuedeAdministrar(r Rol) bool {
	return r == RolSuperAdmin || r == RolAdmin
}

// PuedeExportar el reporte semanal.
func PuedeExportar(r Rol) bool {
	return r != RolGestor
}
