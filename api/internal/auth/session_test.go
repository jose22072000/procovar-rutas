package auth

import (
	"testing"

	"github.com/procovar/procovar-rutas/api/internal/scope"
)

func TestMapearRolReconoceLasVariantes(t *testing.T) {
	casos := map[string]scope.Role{
		"Administrador": scope.RoleAdmin,
		"admin":         scope.RoleAdmin,
		"SUPERVISOR":    scope.RoleSupervisor,
		"Gestor":        scope.RoleAgent,
		"gerente":       scope.RoleManager,
		"super_admin":   scope.RoleSuperAdmin,
	}
	for entrada, esperado := range casos {
		if got := MapRole([]string{entrada}, false); got != esperado {
			t.Errorf("MapRole(%q) = %q, se esperaba %q", entrada, got, esperado)
		}
	}
}

// A role not mapped here must NOT grant access by default: that is exactly how
// permissions leak in when someone adds a new role in procovar-auth.
func TestRolDesconocidoNoDaAcceso(t *testing.T) {
	if got := MapRole([]string{"coordinador_de_flota"}, false); got != "" {
		t.Errorf("= %q, se esperaba ninguno", got)
	}
}

func TestConVariosRolesGanaElMasAmplio(t *testing.T) {
	got := MapRole([]string{"supervisor", "administrador"}, false)
	if got != scope.RoleAdmin {
		t.Errorf("= %q, se esperaba admin", got)
	}
}

func TestAdminDeSistemaEsSuperAdmin(t *testing.T) {
	if got := MapRole([]string{"gestor"}, true); got != scope.RoleSuperAdmin {
		t.Errorf("= %q", got)
	}
}

func TestMembresiaActivaRespetaLaOrganizacionDeLaSesion(t *testing.T) {
	s := &Session{
		Memberships: []Membresia{
			{Roles: []string{"gestor"}, Organization: Organization{ID: "org-hol", Name: "Holguín"}},
			{Roles: []string{"admin"}, Organization: Organization{ID: "org-cmg", Name: "Camagüey"}},
		},
	}
	s.Session.ActiveOrganizationID = "org-cmg"

	m := s.ActiveMembership()
	if m == nil || m.Organization.ID != "org-cmg" {
		t.Fatalf("membresía = %+v", m)
	}
	if id := s.Translate(); id.Role != scope.RoleAdmin || id.AuthOrgID != "org-cmg" {
		t.Errorf("identidad = %+v", id)
	}
}

func TestTraducirCaeAlRbacSiLaMembresiaNoTraeRolConocido(t *testing.T) {
	s := &Session{
		Memberships: []Membresia{{Roles: []string{"algo_raro"}, Organization: Organization{ID: "org-cmg"}}},
	}
	s.Rbac.Roles = []string{"supervisor"}

	if id := s.Translate(); id.Role != scope.RoleSupervisor {
		t.Errorf("rol = %q", id.Role)
	}
}

func TestSinMembresiasNiRolNoHayAcceso(t *testing.T) {
	s := &Session{}
	if id := s.Translate(); id.Role != "" {
		t.Errorf("rol = %q, se esperaba ninguno", id.Role)
	}
}
