package auth

import (
	"strings"

	"github.com/procovar/procovar-rutas/api/internal/scope"
)

// Traducción de los roles de procovar-auth a los de esta aplicación.
//
// El catálogo de roles de procovar-auth es único para todo Procovar (Super
// Admin, Administrador, Supervisor, Gestor, Operador) y viene con nombres
// escritos para personas. Aquí se normalizan.
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

// jerarquía de mayor a menor scope. Quien tenga varios roles se queda con el
// más amplio: es lo que ya espera un supervisor que además es administrador.
var jerarquia = []scope.Role{
	scope.RoleSuperAdmin,
	scope.RoleAdmin,
	scope.RoleManager,
	scope.RoleSupervisor,
	scope.RoleAgent,
}

// MapearRol traduce la lista de roles de una membresía.
//
// Devuelve cadena vacía cuando ninguno es reconocible: sin rol conocido no se
// entra. Un rol nuevo en procovar-auth que aquí no esté mapeado NO debe conceder
// acceso por descarte — eso es exactamente cómo se cuelan los permisos.
func MapearRol(roles []string, esAdminDeSistema bool) scope.Role {
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

// MembresiaActiva elige con qué sucursal entra alguien que pertenece a varias.
//
// Manda la organización activa de la sesión; si no hay ninguna marcada y solo
// pertenece a una, esa. Con varias y ninguna activa se devuelve la primera, que
// es lo que hace el propio procovar-auth.
func (s *Session) MembresiaActiva() *Membresia {
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

// Identity es la sesión ya traducida al vocabulario de esta aplicación, salvo
// el trabajadorId, que sale de la base local.
type Identity struct {
	AuthUserID string
	Email      string
	Name       string
	Role       scope.Role
	AuthOrgID  string
}

// Traducir convierte la respuesta de procovar-auth en una identidad de aquí.
func (s *Session) Traducir() Identity {
	id := Identity{
		AuthUserID: s.User.ID,
		Email:      s.User.Email,
		Name:       s.User.Name,
	}

	if m := s.MembresiaActiva(); m != nil {
		id.AuthOrgID = m.Organization.ID
		id.Role = MapearRol(m.Roles, s.User.IsSystemAdmin)
	} else {
		id.Role = MapearRol(nil, s.User.IsSystemAdmin)
	}

	// El RBAC resuelto de procovar-auth es la segunda fuente: si la membresía no
	// traía un rol reconocible, se mira ahí antes de rendirse.
	if id.Role == "" {
		id.Role = MapearRol(s.Rbac.Roles, s.User.IsSystemAdmin)
	}

	return id
}

// AlcanceDe construye la sesión que consume internal/scope.
func (i Identity) AlcanceDe(trabajadorID, sucursalID string) scope.Session {
	return scope.Session{
		AuthUserID: i.AuthUserID,
		SellerID:   trabajadorID,
		BranchID:   sucursalID,
		Role:       i.Role,
	}
}
