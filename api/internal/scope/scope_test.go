package scope

import (
	"errors"
	"testing"
	"time"
)

var (
	agosto  = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	octubre = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
)

func fecha(a, m, d int) *time.Time {
	t := time.Date(a, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return &t
}

// Yasmani supervisaba a Alexander en agosto; en septiembre pasó al equipo de Dania.
func vigencias() []Term {
	return []Term{
		{ManagerID: "t-alex", SupervisorID: "t-yas", From: *fecha(2026, 1, 1), To: fecha(2026, 8, 31)},
		{ManagerID: "t-alex", SupervisorID: "t-dania", From: *fecha(2026, 9, 1)},
		{ManagerID: "t-luis", SupervisorID: "t-yas", From: *fecha(2026, 1, 1)},
	}
}

func sesion(rol Role) Session {
	return Session{AuthUserID: "u-1", SellerID: "t-yas", BranchID: "s-cmg", Role: rol}
}

func TestSuperAdminVeTodoMenosASiMismo(t *testing.T) {
	f, err := Compute(sesion(RoleSuperAdmin), agosto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.BranchID != "" || f.SellerNot != "t-yas" || f.Empty {
		t.Errorf("= %+v", f)
	}
}

func TestAdminYGerenteSeQuedanEnSuSucursal(t *testing.T) {
	for _, rol := range []Role{RoleAdmin, RoleManager} {
		f, err := Compute(sesion(rol), agosto, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f.BranchID != "s-cmg" || f.SellerNot != "t-yas" {
			t.Errorf("%s = %+v", rol, f)
		}
	}
}

func TestGestorNoEntra(t *testing.T) {
	if _, err := Compute(sesion(RoleAgent), agosto, nil); !errors.Is(err, ErrNoAccess) {
		t.Errorf("err = %v, se esperaba ErrNoAccess", err)
	}
}

func TestSupervisorVeASuEquipoYNuncaASiMismo(t *testing.T) {
	f, err := Compute(sesion(RoleSupervisor), agosto, vigencias())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.SellersIn) != 2 {
		t.Fatalf("equipo = %v", f.SellersIn)
	}
	for _, id := range f.SellersIn {
		if id == "t-yas" {
			t.Error("un supervisor no debe verse a sí mismo")
		}
	}
}

// El corazón de las vigencias: el equipo se evalúa contra la fecha CONSULTADA.
func TestElEquipoSeEvaluaContraLaFechaConsultada(t *testing.T) {
	v := vigencias()

	enAgosto, _ := Compute(sesion(RoleSupervisor), agosto, v)
	if !contiene(enAgosto.SellersIn, "t-alex") {
		t.Error("en agosto, Alexander era de Yasmani")
	}

	enOctubre, _ := Compute(sesion(RoleSupervisor), octubre, v)
	if contiene(enOctubre.SellersIn, "t-alex") {
		t.Error("en octubre, Alexander ya no es de Yasmani")
	}
	if !contiene(enOctubre.SellersIn, "t-luis") {
		t.Error("Luis sigue siendo de Yasmani")
	}

	dania := sesion(RoleSupervisor)
	dania.SellerID = "t-dania"
	deDania, _ := Compute(dania, octubre, v)
	if len(deDania.SellersIn) != 1 || deDania.SellersIn[0] != "t-alex" {
		t.Errorf("Dania debería ver a Alexander en octubre: %v", deDania.SellersIn)
	}
}

// Falla cerrado: ante la duda, no se ve nada.
func TestSupervisorSinEquipoNoVeNada(t *testing.T) {
	s := sesion(RoleSupervisor)
	s.SellerID = "t-nadie"
	f, _ := Compute(s, agosto, vigencias())
	if !f.Empty {
		t.Errorf("= %+v", f)
	}
}

func TestAdminSinSucursalNoVeNada(t *testing.T) {
	s := sesion(RoleAdmin)
	s.BranchID = ""
	f, _ := Compute(s, agosto, nil)
	if !f.Empty {
		t.Errorf("= %+v", f)
	}
}

func TestSupervisorSinFichaLocalNoVeNada(t *testing.T) {
	s := sesion(RoleSupervisor)
	s.SellerID = ""
	f, _ := Compute(s, agosto, vigencias())
	if !f.Empty {
		t.Errorf("= %+v", f)
	}
}

func contiene(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// El gerente está por encima del supervisor y ve toda su sucursal, pero NO
// administra: no toca carpetas de Drive, ni alias, ni umbrales.
func TestGerenteVeLaSucursalPeroNoAdministra(t *testing.T) {
	f, err := Compute(sesion(RoleManager), agosto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.BranchID != "s-cmg" || len(f.SellersIn) != 0 {
		t.Errorf("= %+v; el gerente ve la sucursal entera, no un equipo", f)
	}
	if PuedeAdministrar(RoleManager) {
		t.Error("el gerente no administra")
	}
	if !PuedeAdministrar(RoleAdmin) || !PuedeAdministrar(RoleSuperAdmin) {
		t.Error("admin y super admin sí administran")
	}
	if !CanExport(RoleManager) {
		t.Error("el gerente sí exporta reportes")
	}
}
