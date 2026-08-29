package api

import "testing"

// Los nombres REALES de producción, que son los que engañan. Cuando esto falla no salta
// nada: la persona entra, no ve nada, y no hay forma de saber que el problema es que su
// sucursal se llama distinto en cada aplicación.
func candidatasReales() []SucursalCandidata {
	return []SucursalCandidata{
		{ID: "bay", Clave: "bayamo"},
		{ID: "cam", Clave: "camaguey"},
		{ID: "gtm", Clave: "guantanamo"},
		{ID: "hab", Clave: "habana"},
		{ID: "hol", Clave: "holguin"},
		{ID: "tun", Clave: "lastunas"},
		{ID: "ss", Clave: "santispiritus"},
		{ID: "stg", Clave: "santiago"},
	}
}

func TestEmparejarNombresDeProduccion(t *testing.T) {
	casos := map[string]string{
		"La Habana":       "hab", // «lahabana» contiene «habana»
		"Sancti Spíritus": "ss",  // una letra de diferencia: santispiritus / sanctispiritus
		"Santiago de Cuba": "stg",
		"Camagüey":        "cam",
		"Granma":          "bay", // la pareja escrita a mano: Granma es Bayamo
		"Guantánamo":      "gtm",
		"Holguín":         "hol",
		"Las Tunas":       "tun",
	}

	for nombre, esperado := range casos {
		if id := EmparejarSucursal(nombre, "", candidatasReales()); id != esperado {
			t.Fatalf("%q emparejó con %q y tenía que ser %q", nombre, id, esperado)
		}
	}
}

func TestPrefiereElCodigoCuandoLoHay(t *testing.T) {
	con := []SucursalCandidata{{ID: "hab", Clave: "otracosa", Codigo: "HAB"}}

	if id := EmparejarSucursal("La Habana", "hab", con); id != "hab" {
		t.Fatalf("el código tiene que mandar, y devolvió %q", id)
	}
}

func TestNoEmparejaSiNoEstaClaro(t *testing.T) {
	// Darle a alguien la sucursal de otro es peor que dejarlo sin ninguna: se ve trabajo
	// ajeno sin saberlo. Ante la duda, nada.
	if id := EmparejarSucursal("Moa", "", candidatasReales()); id != "" {
		t.Fatalf("Moa no está entre las de aquí y emparejó con %q", id)
	}
	if id := EmparejarSucursal("Villa Clara", "", candidatasReales()); id != "" {
		t.Fatalf("Villa Clara emparejó con %q", id)
	}
}
