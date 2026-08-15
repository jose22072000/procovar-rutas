package gpx

import "testing"

func TestNormalizarCasaVariantesDelMismoNombre(t *testing.T) {
	esperado := Normalize("José Pérez")
	for _, v := range []string{"jose_perez", "JOSE-PEREZ", "  Jose  Perez  "} {
		if got := Normalize(v); got != esperado {
			t.Errorf("Normalize(%q) = %q, se esperaba %q", v, got, esperado)
		}
	}
	if Normalize("Camagüey") != "camaguey" {
		t.Errorf("la diéresis debería caer: %q", Normalize("Camagüey"))
	}
}

func aliasDePrueba() map[string]string {
	return map[string]string{
		Normalize("Alexander"): "t-alex",
		Normalize("Yasmani"):   "t-yas",
	}
}

func TestResolverLaCarpetaDeVendedorMandaSobreTodo(t *testing.T) {
	r := ResolveSeller(Context{
		SourceType:     SourceSeller,
		SourceSellerID: "t-yas",
		FileName:       "alexander_2026-08-10.gpx",
		Alias:          aliasDePrueba(),
	})
	if r.SellerID != "t-yas" || r.Via != ViaFuente {
		t.Errorf("= %+v", r)
	}
}

func TestResolverUsaLaSubcarpetaMasInterna(t *testing.T) {
	r := ResolveSeller(Context{
		SourceType: SourceBranch,
		FolderPath: []string{"Camaguey", "Alexander"},
		FileName:   "ruta.gpx",
		Alias:      aliasDePrueba(),
	})
	if r.SellerID != "t-alex" || r.Via != ViaCarpeta {
		t.Errorf("= %+v", r)
	}
}

func TestResolverPorNombreIgnorandoLaFecha(t *testing.T) {
	r := ResolveSeller(Context{
		SourceType: SourceBranch,
		FileName:   "Alexander_2026-08-10.gpx",
		Alias:      aliasDePrueba(),
	})
	if r.SellerID != "t-alex" || r.Via != ViaFichero {
		t.Errorf("= %+v", r)
	}
}

func TestResolverCaeAlContenidoDelGpx(t *testing.T) {
	r := ResolveSeller(Context{
		SourceType: SourceBranch,
		FileName:   "track_001.gpx",
		GpxHints:   []string{"YASMANI"},
		Alias:      aliasDePrueba(),
	})
	if r.SellerID != "t-yas" || r.Via != ViaGpx {
		t.Errorf("= %+v", r)
	}
}

// When no rule hits, the file goes to the inbox WITH the hint the admin has to
// match. No file is lost in silence.
func TestResolverMandaALaBandejaConPistaUtil(t *testing.T) {
	r := ResolveSeller(Context{
		SourceType: SourceBranch,
		FileName:   "track_001.gpx",
		GpxHints:   []string{"Redmi Note 12"},
		Alias:      aliasDePrueba(),
	})
	if r.SellerID != "" {
		t.Errorf("no debería resolver: %+v", r)
	}
	if r.Hint != "Redmi Note 12" {
		t.Errorf("pista = %q; es lo que verá el admin en la bandeja", r.Hint)
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
		if got := DateFromName(nombre); got != esperado {
			t.Errorf("DateFromName(%q) = %q, se esperaba %q", nombre, got, esperado)
		}
	}
}

// In Procovar's real setup each shared folder IS a seller's GPS profile ("GPS
// Diana Acosta", "STGGari"), and the files inside carry only the date. Without
// looking at the folder's own name there would be nowhere to get the seller from
// and EVERYTHING would end up in the inbox.
func TestResolverPorElNombreDeLaCarpetaDadaDeAlta(t *testing.T) {
	alias := map[string]string{Normalize("GPS Diana Acosta"): "t-diana"}
	r := ResolveSeller(Context{
		SourceType: SourceMixed,
		SourceName: "GPS Diana Acosta",
		FileName:   "20260812.gpx",
		Alias:      alias,
	})
	if r.SellerID != "t-diana" || r.Via != ViaCarpeta {
		t.Errorf("= %+v", r)
	}
}

func TestCarpetaSinCasarLlevaSuNombreALaBandeja(t *testing.T) {
	r := ResolveSeller(Context{
		SourceType: SourceMixed,
		SourceName: "TABLET3",
		FileName:   "20260812.gpx",
		Alias:      map[string]string{},
	})
	if r.SellerID != "" || r.Hint != "TABLET3" {
		t.Errorf("= %+v", r)
	}
}
