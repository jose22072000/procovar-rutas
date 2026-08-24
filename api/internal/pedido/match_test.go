package pedido

import "testing"

// Los nombres de estas pruebas son los reales del panel: carpetas de Drive
// ("ALEXANDER", "Alendy Torres GPS", "STGTadyslai") contra vendedores del maestro
// de PEDIDO ("andy.almanza"). Lo que se está fijando aquí es cuándo el sistema se
// atreve a decir que dos nombres son la misma persona.

func casaCon(t *testing.T, carpeta, vendedor, codigo string) {
	t.Helper()
	if !casan(carpeta, vendedor, codigo) {
		t.Errorf("%q debería casar con %q (%q) y no casa", carpeta, vendedor, codigo)
	}
}

func noCasaCon(t *testing.T, carpeta, vendedor, codigo string) {
	t.Helper()
	if casan(carpeta, vendedor, codigo) {
		t.Errorf("%q NO debería casar con %q (%q) y casa", carpeta, vendedor, codigo)
	}
}

func TestCasanNombres(t *testing.T) {
	// El nombre entero, con o sin tildes, con o sin mayúsculas.
	casaCon(t, "ANDY ALMANZA", "Andy Almanza", "andy.almanza")
	casaCon(t, "jose_perez", "José Pérez", "jose.perez")

	// El código, tal cual.
	casaCon(t, "andy.almanza", "Andy Almanza Pérez", "andy.almanza")

	// Un nombre de pila que cae dentro del nombre completo: el caso más común, las
	// carpetas están llamadas por el nombre de pila.
	casaCon(t, "ALEXANDER", "ALEXANDER RODRÍGUEZ", "alexander.rodriguez")

	// La carpeta con su coletilla de dispositivo.
	casaCon(t, "Alendy Torres GPS", "ALENDY TORRES PÉREZ", "alendy.torres")
	casaCon(t, "GPS Diana Acosta", "Diana Acosta", "diana.acosta")

	// El prefijo de sucursal pegado delante.
	casaCon(t, "STGTadyslai", "Tadyslai Fernández", "tadyslai.fernandez")
}

func TestNoCasanLosQueNoSon(t *testing.T) {
	// Al revés no vale: un apellido suelto no se queda con el vendedor entero.
	noCasaCon(t, "RODRÍGUEZ PÉREZ GARCÍA", "ALEXANDER RODRÍGUEZ", "alexander.rodriguez")

	// Una tableta sin nombre no es nadie.
	noCasaCon(t, "TABLET3", "Andy Almanza", "andy.almanza")

	// Iniciales no: casarían con media plantilla.
	noCasaCon(t, "A", "Andy Almanza", "andy.almanza")
	noCasaCon(t, "GPS", "Andy Almanza", "andy.almanza")

	// Un trozo demasiado corto tras quitar el prefijo tampoco.
	noCasaCon(t, "STGari", "Ariel Gómez", "ariel.gomez")
}

// La regla que de verdad importa: cuando un nombre le vale a dos vendedores, no se
// empareja NINGUNO. Adjudicarle la ruta al Alexander equivocado no deja el panel
// incompleto, lo deja MINTIENDO — y desde la pantalla no hay forma de notarlo.
func TestLaAmbiguedadNoEmpareja(t *testing.T) {
	vendedores := []VendorRef{
		{Code: "alexander.rodriguez", Name: "ALEXANDER RODRÍGUEZ", BranchID: "cam"},
		{Code: "alexander.perez", Name: "ALEXANDER PÉREZ", BranchID: "cam"},
	}
	trabajadores := []SellerRef{
		{ID: "t1", Name: "ALEXANDER", BranchID: "cam"},
	}

	if n := len(MatchVendors(trabajadores, vendedores)); n != 0 {
		t.Fatalf("con dos Alexander no se empareja ninguno, y se emparejaron %d", n)
	}
}

// Dos sucursales distintas no se estorban: un "ALEXANDER" en Camagüey y otro en
// Santiago no son una ambigüedad, son dos personas de dos paneles distintos.
func TestCadaSucursalPorSuCuenta(t *testing.T) {
	vendedores := []VendorRef{
		{Code: "alexander.rodriguez", Name: "ALEXANDER RODRÍGUEZ", BranchID: "cam"},
	}
	trabajadores := []SellerRef{
		{ID: "cam1", Name: "ALEXANDER", BranchID: "cam"},
		{ID: "stg1", Name: "ALEXANDER", BranchID: "stg"},
	}

	parejas := MatchVendors(trabajadores, vendedores)
	if len(parejas) != 1 {
		t.Fatalf("se esperaba 1 pareja, salieron %d", len(parejas))
	}
	if parejas[0].SellerID != "cam1" {
		t.Errorf("se emparejó con %q, y el de esa sucursal es cam1", parejas[0].SellerID)
	}
}

// Un vendedor de PEDIDO cuya sucursal no tiene a nadie parecido se queda suelto, y
// eso es información: sus pedidos no se cruzan con ninguna ruta y el panel lo dice.
func TestElQueNoCasaSeQuedaSuelto(t *testing.T) {
	vendedores := []VendorRef{{Code: "nadie.conocido", Name: "PEPE FANTASMA", BranchID: "cam"}}
	trabajadores := []SellerRef{{ID: "t1", Name: "ALEXANDER", BranchID: "cam"}}

	if n := len(MatchVendors(trabajadores, vendedores)); n != 0 {
		t.Fatalf("no debería emparejar nada, emparejó %d", n)
	}
}
