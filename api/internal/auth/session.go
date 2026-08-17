package auth

import (
	"strings"

	"github.com/procovar/procovar-rutas/api/internal/scope"
)

// Translation of procovar-auth's roles into this application's.
//
// procovar-auth's role catalogue is shared across all of Procovar (Super Admin,
// Administrador, Supervisor, Gestor, Operador) and comes with names written for
// people. They are normalized here.
var equivalencias = map[string]scope.Role{
	"super_admin":   scope.RoleSuperAdmin,
	"superadmin":    scope.RoleSuperAdmin,
	"super admin":   scope.RoleSuperAdmin,
	"admin":         scope.RoleAdmin,
	"administrador": scope.RoleAdmin,
	"gerente":       scope.RoleManager,
	"manager":       scope.RoleManager,
	"supervisor":    scope.RoleSupervisor,
	"gestor":        scope.RoleAgent,
	"vendedor":      scope.RoleAgent,
	"operador":      scope.RoleAgent,
}

// ranked from widest to narrowest scope. Anyone holding several roles keeps the
// widest: that is what a supervisor who is also an administrator already expects.
var jerarquia = []scope.Role{
	scope.RoleSuperAdmin,
	scope.RoleAdmin,
	scope.RoleManager,
	scope.RoleSupervisor,
	scope.RoleAgent,
}

// MapRole translates a membership's list of roles.
//
// It returns an empty string when none is recognisable: no known role, no entry. A
// new role in procovar-auth that is not mapped here must NOT grant access by
// default — that is exactly how permissions leak in.
func MapRole(roles []string, esAdminDeSistema bool) scope.Role {
	if esAdminDeSistema {
		return scope.RoleSuperAdmin
	}

	encontrados := map[scope.Role]bool{}
	for _, r := range roles {
		if rol, ok := equivalencias[strings.ToLower(strings.TrimSpace(r))]; ok {
			encontrados[rol] = true
		}
	}
	for _, rol := range jerarquia {
		if encontrados[rol] {
			return rol
		}
	}
	return ""
}

// ActiveMembership picks which branch someone belonging to several comes in with.
//
// The session's active organization wins; if none is marked and they belong to
// just one, that one. With several and none active it returns the first, which is
// what procovar-auth itself does.
func (s *Session) ActiveMembership() *Membresia {
	if len(s.Memberships) == 0 {
		return nil
	}
	if id := s.Session.ActiveOrganizationID; id != "" {
		for i := range s.Memberships {
			if s.Memberships[i].Organization.ID == id {
				return &s.Memberships[i]
			}
		}
	}
	return &s.Memberships[0]
}

// Identity is the session already translated into this application's vocabulary,
// except for the seller id, which comes from the local database.
type Identity struct {
	AuthUserID string
	Email      string
	Name       string
	Role       scope.Role
	AuthOrgID  string
	// Permisos: lo que procovar-auth dice que esta persona puede hacer. Es la
	// autoridad, no una copia de lo que se decida aquí: los permisos se reparten
	// allí, en una sola pantalla, para las seis aplicaciones a la vez.
	Permisos map[string]bool
	// Wildcard: el Super Admin. Puede con todo sin que haya que enumerárselo.
	Wildcard bool
}

// Puede responde si esta persona tiene esa llave.
func (i Identity) Puede(clave string) bool {
	if i.Wildcard {
		return true
	}
	return i.Permisos[clave]
}

// Translate turns procovar-auth's response into an identity for this application.
func (s *Session) Translate() Identity {
	id := Identity{
		AuthUserID: s.User.ID,
		Email:      s.User.Email,
		Name:       s.User.Name,
	}

	if m := s.ActiveMembership(); m != nil {
		id.AuthOrgID = m.Organization.ID
	}

	// El rol viene de procovar-auth, que es donde se reparte.
	//
	// Se mira PRIMERO `role` y `rbac.roles`, que traen el nombre del catálogo
	// ("SUPERVISOR"), y solo después la membresía. Al revés no funcionaba: en la
	// membresía va la columna de better-auth, que dice "owner" o "member" —su
	// vocabulario, no el de Procovar—, así que una supervisora se quedaba sin rol
	// reconocible y sin entrar. Daba 403 y una pantalla en blanco.
	id.Role = MapRole(append([]string{s.Rol}, s.Rbac.Roles...), s.User.IsSystemAdmin)
	if id.Role == "" {
		if m := s.ActiveMembership(); m != nil {
			id.Role = MapRole(m.Roles, s.User.IsSystemAdmin)
		}
	}

	// Y los permisos, tal cual: aquí no se decide nada, se obedece.
	id.Wildcard = s.Rbac.Wildcard || s.User.IsSystemAdmin
	id.Permisos = map[string]bool{}
	for _, claves := range [][]string{s.Rbac.Permissions, s.Rbac.Global} {
		for _, k := range claves {
			id.Permisos[k] = true
		}
	}

	return id
}

// ScopeOf builds the session internal/scope consumes.
func (i Identity) ScopeOf(trabajadorID, sucursalID string) scope.Session {
	return scope.Session{
		AuthUserID: i.AuthUserID,
		SellerID:   trabajadorID,
		BranchID:   sucursalID,
		Role:       i.Role,
	}
}
