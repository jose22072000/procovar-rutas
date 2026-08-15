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
		id.Role = MapRole(m.Roles, s.User.IsSystemAdmin)
	} else {
		id.Role = MapRole(nil, s.User.IsSystemAdmin)
	}

	// procovar-auth's resolved RBAC is the second source: if the membership carried
	// no recognisable role, it is checked before giving up.
	if id.Role == "" {
		id.Role = MapRole(s.Rbac.Roles, s.User.IsSystemAdmin)
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
