// Package scope decides who is allowed to see whose routes.
//
// Every query in the system is required to go through here. A filter hand-copied
// into twelve handlers is a filter someone will forget in the thirteenth — and in
// this application that lapse means a supervisor sees another team's route, or
// their own.
//
// Two rules that are not negotiable:
//
//  1. Nobody sees their own route. This is a supervision tool: whoever is looking
//     is not the subject of the look.
//  2. A supervisor's scope is evaluated against the DATE BEING QUERIED, not
//     against today. Ask in October for the week of August and you see the sellers
//     you supervised in August. That is why supervision carries terms.
package scope

import (
	"errors"
	"sort"
	"time"
)

// Role is the person's role at Procovar. It comes from procovar-auth.
type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleAdmin      Role = "admin"
	RoleManager    Role = "gerente"
	RoleSupervisor Role = "supervisor"
	RoleAgent      Role = "gestor"
)

// ErrNoAccess is what an agent gets: they do not use this application. It is an
// error and not an empty result so the response is a 403 rather than a blank
// screen that looks like the system is broken.
var ErrNoAccess = errors.New("este rol no tiene acceso a los recorridos")

// Session is the identity already verified against procovar-auth.
type Session struct {
	AuthUserID string
	// SellerID is their local record, if they have one. That is what gets
	// excluded.
	SellerID string
	BranchID string
	Role     Role
}

// Term is "this person supervised that one between these dates".
type Term struct {
	ManagerID    string
	SupervisorID string
	From         time.Time
	// To nil = still in force.
	To *time.Time
}

// Filter is the clause every query must apply.
type Filter struct {
	// Empty BranchID = no branch restriction.
	BranchID string
	// SellersIn, when not nil, limits the result to those sellers.
	SellersIn []string
	// SellerNot excludes one person: always the one doing the query.
	SellerNot string
	// Empty = this role may see nothing; the query must return zero rows.
	Empty bool
}

// ActiveAgents are the sellers under a supervisor on a given date.
func ActiveAgents(vigencias []Term, supervisorID string, fecha time.Time) []string {
	ids := []string{}
	visto := map[string]bool{}
	for _, v := range vigencias {
		if v.SupervisorID != supervisorID || visto[v.ManagerID] {
			continue
		}
		if v.From.After(fecha) {
			continue
		}
		if v.To != nil && v.To.Before(fecha) {
			continue
		}
		visto[v.ManagerID] = true
		ids = append(ids, v.ManagerID)
	}
	sort.Strings(ids)
	return ids
}

// Compute returns the scope filter.
//
// `date` is the date of the data being asked for, not today's. `terms` is only
// needed for the supervisor role; the others never look at it.
func Compute(s Session, fecha time.Time, vigencias []Term) (Filter, error) {
	switch s.Role {
	case RoleSuperAdmin:
		// Everything. They are excluded from their own results in case they are
		// also a seller at some branch.
		return Filter{SellerNot: s.SellerID}, nil

	case RoleAdmin, RoleManager:
		if s.BranchID == "" {
			return Filter{Empty: true}, nil
		}
		return Filter{BranchID: s.BranchID, SellerNot: s.SellerID}, nil

	case RoleSupervisor:
		// Con equipo asignado, su equipo. Sin equipo asignado, SU SUCURSAL.
		//
		// Antes, sin vigencias no veía nada, y eso convertía el panel en una pantalla
		// vacía para la única persona que lo iba a usar todos los días: una
		// supervisora de sucursal entra a mirar a los vendedores de su sucursal, y
		// las vigencias —que existen para poder preguntar "¿de quién era Alexander en
		// agosto?"— no están dadas de alta ni hay dónde darlas.
		//
		// No es abrir la mano: no ve otra sucursal, y no se ve a sí misma. En cuanto
		// se le asigne equipo, el equipo manda y esto deja de aplicar.
		suyos := []string{}
		if s.SellerID != "" {
			for _, id := range ActiveAgents(vigencias, s.SellerID, fecha) {
				if id != s.SellerID {
					suyos = append(suyos, id)
				}
			}
		}
		if len(suyos) > 0 {
			return Filter{SellersIn: suyos, SellerNot: s.SellerID}, nil
		}
		if s.BranchID == "" {
			// Sin sucursal no hay a qué limitarla, y devolverlo todo sí sería abrir
			// la mano.
			return Filter{Empty: true}, nil
		}
		return Filter{BranchID: s.BranchID, SellerNot: s.SellerID}, nil

	case RoleAgent:
		return Filter{Empty: true}, ErrNoAccess

	default:
		return Filter{Empty: true}, nil
	}
}

// CanAdminister: Drive sources, aliases and thresholds.
func CanAdminister(r Role) bool {
	return r == RoleSuperAdmin || r == RoleAdmin
}

// CanExport the report.
func CanExport(r Role) bool {
	return r != RoleAgent
}
