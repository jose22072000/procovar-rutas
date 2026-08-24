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

// Yasmani supervised Alexander in August; in September he moved to Dania's team.
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

// The heart of the terms: the team is evaluated against the DATE BEING QUERIED.
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

// Sin equipo asignado, su sucursal.
//
// Estas dos pruebas decían lo contrario —«sin equipo no ve nada»— y llevaban en rojo
// desde que el alcance cambió a propósito: el equipo sale de las vigencias, no hay
// ninguna dada de alta ni pantalla donde darlas, así que la supervisora abría el panel
// y lo encontraba vacío. Se quedaron describiendo lo de antes, que es la peor forma en
// que puede fallar una prueba: la que está en rojo no se lee, se aprende a saltar, y
// con ella dejan de leerse las de al lado.
//
// Cerrar por defecto sigue en pie donde de verdad hace falta: sin sucursal a la que
// limitarla, no ve nada. Es la de abajo.
func TestSupervisorSinEquipoVeSuSucursal(t *testing.T) {
	s := sesion(RoleSupervisor)
	s.SellerID = "t-nadie"
	f, _ := Compute(s, agosto, vigencias())

	if f.Empty {
		t.Fatalf("sin equipo ve su sucursal, no una pantalla vacía: %+v", f)
	}
	if f.BranchID != s.BranchID {
		t.Errorf("tendría que quedar limitada a %q, y quedó en %q", s.BranchID, f.BranchID)
	}
	if len(f.SellersIn) != 0 {
		t.Errorf("sin equipo no hay lista de vendedores que valga: %v", f.SellersIn)
	}
	// Y lo que no se negocia: nadie ve su propio recorrido.
	if f.SellerNot != "t-nadie" {
		t.Errorf("tiene que excluirse a sí misma, y excluye a %q", f.SellerNot)
	}
}

// Fail closed donde toca: sin sucursal no hay a qué limitarla, y devolverlo todo sí
// sería abrir la mano.
func TestSupervisorSinSucursalNoVeNada(t *testing.T) {
	s := sesion(RoleSupervisor)
	s.SellerID = "t-nadie"
	s.BranchID = ""
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

// Sin ficha local tampoco se queda a oscuras: sin ficha no hay equipo que buscar —las
// vigencias se apuntan contra el vendedor, no contra el usuario— así que cae en lo
// mismo que la de arriba y ve su sucursal. Y no hay nada suyo que excluir, porque no
// tiene recorrido propio.
func TestSupervisorSinFichaLocalVeSuSucursal(t *testing.T) {
	s := sesion(RoleSupervisor)
	s.SellerID = ""
	f, _ := Compute(s, agosto, vigencias())

	if f.Empty {
		t.Fatalf("= %+v", f)
	}
	if f.BranchID != s.BranchID {
		t.Errorf("tendría que quedar limitada a %q, y quedó en %q", s.BranchID, f.BranchID)
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

// The manager sits above the supervisor and sees their whole branch, but does NOT
// administer: no Drive folders, no aliases, no thresholds.
func TestGerenteVeLaSucursalPeroNoAdministra(t *testing.T) {
	f, err := Compute(sesion(RoleManager), agosto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.BranchID != "s-cmg" || len(f.SellersIn) != 0 {
		t.Errorf("= %+v; el gerente ve la sucursal entera, no un equipo", f)
	}
	if CanAdminister(RoleManager) {
		t.Error("el gerente no administra")
	}
	if !CanAdminister(RoleAdmin) || !CanAdminister(RoleSuperAdmin) {
		t.Error("admin y super admin sí administran")
	}
	if !CanExport(RoleManager) {
		t.Error("el gerente sí exporta reportes")
	}
}
