package auth

import (
	"testing"

	"github.com/procovar/procovar-rutas/api/internal/alcance"
)

func TestMapearRolReconoceLasVariantes(t *testing.T) {
	casos := map[string]alcance.Rol{
		"Administrador": alcance.RolAdmin,
		"admin":         alcance.RolAdmin,
		"SUPERVISOR":    alcance.RolSupervisor,
		"Gestor":        alcance.RolGestor,
		"gerente":       alcance.RolGerente,
		"super_admin":   alcance.RolSuperAdmin,
	}
	for entrada, esperado := range casos {
		if got := MapearRol([]string{entrada}, false); got != esperado {
			t.Errorf("MapearRol(%q) = %q, se esperaba %q", entrada, got, esperado)
		}
	}
}

// Un rol que aquí no está mapeado NO puede dar acceso por descarte: así es como
// se cuelan los permisos cuando alguien añade un rol nuevo en procovar-auth.
func TestRolDesconocidoNoDaAcceso(t *testing.T) {
	if got := MapearRol([]string{"coordinador_de_flota"}, false); got != "" {
		t.Errorf("= %q, se esperaba ninguno", got)
	}
}

func TestConVariosRolesGanaElMasAmplio(t *testing.T) {
	got := MapearRol([]string{"supervisor", "administrador"}, false)
	if got != alcance.RolAdmin {
		t.Errorf("= %q, se esperaba admin", got)
	}
}

func TestAdminDeSistemaEsSuperAdmin(t *testing.T) {
	if got := MapearRol([]string{"gestor"}, true); got != alcance.RolSuperAdmin {
		t.Errorf("= %q", got)
	}
}

func TestMembresiaActivaRespetaLaOrganizacionDeLaSesion(t *testing.T) {
	s := &Sesion{
		Memberships: []Membresia{
			{Roles: []string{"gestor"}, Organization: Organizacion{ID: "org-hol", Name: "Holguín"}},
			{Roles: []string{"admin"}, Organization: Organizacion{ID: "org-cmg", Name: "Camagüey"}},
		},
	}
	s.Session.ActiveOrganizationID = "org-cmg"

	m := s.MembresiaActiva()
	if m == nil || m.Organization.ID != "org-cmg" {
		t.Fatalf("membresía = %+v", m)
	}
	if id := s.Traducir(); id.Rol != alcance.RolAdmin || id.AuthOrgID != "org-cmg" {
		t.Errorf("identidad = %+v", id)
	}
}

func TestTraducirCaeAlRbacSiLaMembresiaNoTraeRolConocido(t *testing.T) {
	s := &Sesion{
		Memberships: []Membresia{{Roles: []string{"algo_raro"}, Organization: Organizacion{ID: "org-cmg"}}},
	}
	s.Rbac.Roles = []string{"supervisor"}

	if id := s.Traducir(); id.Rol != alcance.RolSupervisor {
		t.Errorf("rol = %q", id.Rol)
	}
}

func TestSinMembresiasNiRolNoHayAcceso(t *testing.T) {
	s := &Sesion{}
	if id := s.Traducir(); id.Rol != "" {
		t.Errorf("rol = %q, se esperaba ninguno", id.Rol)
	}
}
