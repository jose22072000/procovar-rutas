package gpx

import "testing"

func TestNormalizarCasaVariantesDelMismoNombre(t *testing.T) {
	esperado := Normalizar("José Pérez")
	for _, v := range []string{"jose_perez", "JOSE-PEREZ", "  Jose  Perez  "} {
		if got := Normalizar(v); got != esperado {
			t.Errorf("Normalizar(%q) = %q, se esperaba %q", v, got, esperado)
		}
	}
	if Normalizar("Camagüey") != "camaguey" {
		t.Errorf("la diéresis debería caer: %q", Normalizar("Camagüey"))
	}
}

func aliasDePrueba() map[string]string {
	return map[string]string{
		Normalizar("Alexander"): "t-alex",
		Normalizar("Yasmani"):   "t-yas",
	}
}

func TestResolverLaCarpetaDeVendedorMandaSobreTodo(t *testing.T) {
	r := ResolverTrabajador(Contexto{
		TipoFuente:         FuenteVendedor,
		TrabajadorIDFuente: "t-yas",
		NombreFichero:      "alexander_2026-08-10.gpx",
		Alias:              aliasDePrueba(),
	})
	if r.TrabajadorID != "t-yas" || r.Via != ViaFuente {
		t.Errorf("= %+v", r)
	}
}

func TestResolverUsaLaSubcarpetaMasInterna(t *testing.T) {
	r := ResolverTrabajador(Contexto{
		TipoFuente:    FuenteSucursal,
		RutaCarpeta:   []string{"Camaguey", "Alexander"},
		NombreFichero: "ruta.gpx",
		Alias:         aliasDePrueba(),
	})
	if r.TrabajadorID != "t-alex" || r.Via != ViaCarpeta {
		t.Errorf("= %+v", r)
	}
}

func TestResolverPorNombreIgnorandoLaFecha(t *testing.T) {
	r := ResolverTrabajador(Contexto{
		TipoFuente:    FuenteSucursal,
		NombreFichero: "Alexander_2026-08-10.gpx",
		Alias:         aliasDePrueba(),
	})
	if r.TrabajadorID != "t-alex" || r.Via != ViaFichero {
		t.Errorf("= %+v", r)
	}
}

func TestResolverCaeAlContenidoDelGpx(t *testing.T) {
	r := ResolverTrabajador(Contexto{
		TipoFuente:    FuenteSucursal,
		NombreFichero: "track_001.gpx",
		PistasGpx:     []string{"YASMANI"},
		Alias:         aliasDePrueba(),
	})
	if r.TrabajadorID != "t-yas" || r.Via != ViaGpx {
		t.Errorf("= %+v", r)
	}
}

// Cuando ninguna regla acierta, el fichero va a la bandeja CON la pista que el
// admin tiene que casar. Ningún fichero se pierde en silencio.
func TestResolverMandaALaBandejaConPistaUtil(t *testing.T) {
	r := ResolverTrabajador(Contexto{
		TipoFuente:    FuenteSucursal,
		NombreFichero: "track_001.gpx",
		PistasGpx:     []string{"Redmi Note 12"},
		Alias:         aliasDePrueba(),
	})
	if r.TrabajadorID != "" {
		t.Errorf("no debería resolver: %+v", r)
	}
	if r.Pista != "Redmi Note 12" {
		t.Errorf("pista = %q; es lo que verá el admin en la bandeja", r.Pista)
	}
}

func TestFechaDelNombre(t *testing.T) {
	casos := map[string]string{
		"RUTA_2026-08-15.gpx":      "2026-08-15",
		"20260815_alexander.gpx":   "2026-08-15",
		"15-08-2026.gpx":           "2026-08-15",
		"alexander_15/08/2026.gpx": "2026-08-15",
		"track_001.gpx":            "",
		"ruta.gpx":                 "",
	}
	for nombre, esperado := range casos {
		if got := FechaDelNombre(nombre); got != esperado {
			t.Errorf("FechaDelNombre(%q) = %q, se esperaba %q", nombre, got, esperado)
		}
	}
}
