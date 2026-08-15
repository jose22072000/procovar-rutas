package alcance

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
func vigencias() []Vigencia {
	return []Vigencia{
		{GestorID: "t-alex", SupervisorID: "t-yas", Desde: *fecha(2026, 1, 1), Hasta: fecha(2026, 8, 31)},
		{GestorID: "t-alex", SupervisorID: "t-dania", Desde: *fecha(2026, 9, 1)},
		{GestorID: "t-luis", SupervisorID: "t-yas", Desde: *fecha(2026, 1, 1)},
	}
}

func sesion(rol Rol) Sesion {
	return Sesion{AuthUserID: "u-1", TrabajadorID: "t-yas", SucursalID: "s-cmg", Rol: rol}
}

func TestSuperAdminVeTodoMenosASiMismo(t *testing.T) {
	f, err := Calcular(sesion(RolSuperAdmin), agosto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.SucursalID != "" || f.TrabajadorNot != "t-yas" || f.Vacio {
		t.Errorf("= %+v", f)
	}
}

func TestAdminYGerenteSeQuedanEnSuSucursal(t *testing.T) {
	for _, rol := range []Rol{RolAdmin, RolGerente} {
		f, err := Calcular(sesion(rol), agosto, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f.SucursalID != "s-cmg" || f.TrabajadorNot != "t-yas" {
			t.Errorf("%s = %+v", rol, f)
		}
	}
}

func TestGestorNoEntra(t *testing.T) {
	if _, err := Calcular(sesion(RolGestor), agosto, nil); !errors.Is(err, ErrSinAcceso) {
		t.Errorf("err = %v, se esperaba ErrSinAcceso", err)
	}
}

func TestSupervisorVeASuEquipoYNuncaASiMismo(t *testing.T) {
	f, err := Calcular(sesion(RolSupervisor), agosto, vigencias())
	if err != nil {
		t.Fatal(err)
	}
	if len(f.TrabajadoresIn) != 2 {
		t.Fatalf("equipo = %v", f.TrabajadoresIn)
	}
	for _, id := range f.TrabajadoresIn {
		if id == "t-yas" {
			t.Error("un supervisor no debe verse a sí mismo")
		}
	}
}

// El corazón de las vigencias: el equipo se evalúa contra la fecha CONSULTADA.
func TestElEquipoSeEvaluaContraLaFechaConsultada(t *testing.T) {
	v := vigencias()

	enAgosto, _ := Calcular(sesion(RolSupervisor), agosto, v)
	if !contiene(enAgosto.TrabajadoresIn, "t-alex") {
		t.Error("en agosto, Alexander era de Yasmani")
	}

	enOctubre, _ := Calcular(sesion(RolSupervisor), octubre, v)
	if contiene(enOctubre.TrabajadoresIn, "t-alex") {
		t.Error("en octubre, Alexander ya no es de Yasmani")
	}
	if !contiene(enOctubre.TrabajadoresIn, "t-luis") {
		t.Error("Luis sigue siendo de Yasmani")
	}

	dania := sesion(RolSupervisor)
	dania.TrabajadorID = "t-dania"
	deDania, _ := Calcular(dania, octubre, v)
	if len(deDania.TrabajadoresIn) != 1 || deDania.TrabajadoresIn[0] != "t-alex" {
		t.Errorf("Dania debería ver a Alexander en octubre: %v", deDania.TrabajadoresIn)
	}
}

// Falla cerrado: ante la duda, no se ve nada.
func TestSupervisorSinEquipoNoVeNada(t *testing.T) {
	s := sesion(RolSupervisor)
	s.TrabajadorID = "t-nadie"
	f, _ := Calcular(s, agosto, vigencias())
	if !f.Vacio {
		t.Errorf("= %+v", f)
	}
}

func TestAdminSinSucursalNoVeNada(t *testing.T) {
	s := sesion(RolAdmin)
	s.SucursalID = ""
	f, _ := Calcular(s, agosto, nil)
	if !f.Vacio {
		t.Errorf("= %+v", f)
	}
}

func TestSupervisorSinFichaLocalNoVeNada(t *testing.T) {
	s := sesion(RolSupervisor)
	s.TrabajadorID = ""
	f, _ := Calcular(s, agosto, vigencias())
	if !f.Vacio {
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
	f, err := Calcular(sesion(RolGerente), agosto, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.SucursalID != "s-cmg" || len(f.TrabajadoresIn) != 0 {
		t.Errorf("= %+v; el gerente ve la sucursal entera, no un equipo", f)
	}
	if PuedeAdministrar(RolGerente) {
		t.Error("el gerente no administra")
	}
	if !PuedeAdministrar(RolAdmin) || !PuedeAdministrar(RolSuperAdmin) {
		t.Error("admin y super admin sí administran")
	}
	if !PuedeExportar(RolGerente) {
		t.Error("el gerente sí exporta reportes")
	}
}
