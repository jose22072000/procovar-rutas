package auth

import (
	"strings"

	"github.com/procovar/procovar-rutas/api/internal/alcance"
)

// Traducción de los roles de procovar-auth a los de esta aplicación.
//
// El catálogo de roles de procovar-auth es único para todo Procovar (Super
// Admin, Administrador, Supervisor, Gestor, Operador) y viene con nombres
// escritos para personas. Aquí se normalizan.
var equivalencias = map[string]alcance.Rol{
	"super_admin":   alcance.RolSuperAdmin,
	"superadmin":    alcance.RolSuperAdmin,
	"super admin":   alcance.RolSuperAdmin,
	"admin":         alcance.RolAdmin,
	"administrador": alcance.RolAdmin,
	"gerente":       alcance.RolGerente,
	"manager":       alcance.RolGerente,
	"supervisor":    alcance.RolSupervisor,
	"gestor":        alcance.RolGestor,
	"vendedor":      alcance.RolGestor,
	"operador":      alcance.RolGestor,
}

// jerarquía de mayor a menor alcance. Quien tenga varios roles se queda con el
// más amplio: es lo que ya espera un supervisor que además es administrador.
var jerarquia = []alcance.Rol{
	alcance.RolSuperAdmin,
	alcance.RolAdmin,
	alcance.RolGerente,
	alcance.RolSupervisor,
	alcance.RolGestor,
}

// MapearRol traduce la lista de roles de una membresía.
//
// Devuelve cadena vacía cuando ninguno es reconocible: sin rol conocido no se
// entra. Un rol nuevo en procovar-auth que aquí no esté mapeado NO debe conceder
// acceso por descarte — eso es exactamente cómo se cuelan los permisos.
func MapearRol(roles []string, esAdminDeSistema bool) alcance.Rol {
	if esAdminDeSistema {
		return alcance.RolSuperAdmin
	}

	encontrados := map[alcance.Rol]bool{}
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
func (s *Sesion) MembresiaActiva() *Membresia {
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

// Identidad es la sesión ya traducida al vocabulario de esta aplicación, salvo
// el trabajadorId, que sale de la base local.
type Identidad struct {
	AuthUserID string
	Email      string
	Nombre     string
	Rol        alcance.Rol
	AuthOrgID  string
}

// Traducir convierte la respuesta de procovar-auth en una identidad de aquí.
func (s *Sesion) Traducir() Identidad {
	id := Identidad{
		AuthUserID: s.User.ID,
		Email:      s.User.Email,
		Nombre:     s.User.Name,
	}

	if m := s.MembresiaActiva(); m != nil {
		id.AuthOrgID = m.Organization.ID
		id.Rol = MapearRol(m.Roles, s.User.IsSystemAdmin)
	} else {
		id.Rol = MapearRol(nil, s.User.IsSystemAdmin)
	}

	// El RBAC resuelto de procovar-auth es la segunda fuente: si la membresía no
	// traía un rol reconocible, se mira ahí antes de rendirse.
	if id.Rol == "" {
		id.Rol = MapearRol(s.Rbac.Roles, s.User.IsSystemAdmin)
	}

	return id
}

// AlcanceDe construye la sesión que consume internal/alcance.
func (i Identidad) AlcanceDe(trabajadorID, sucursalID string) alcance.Sesion {
	return alcance.Sesion{
		AuthUserID:   i.AuthUserID,
		TrabajadorID: trabajadorID,
		SucursalID:   sucursalID,
		Rol:          i.Rol,
	}
}
