package pedido

import (
	"strings"

	"github.com/procovar/procovar-rutas/api/internal/gpx"
)

// Pairing the two worlds: PEDIDO's vendor and Rutas' seller.
//
// They are born in different places and nobody ever made them agree. In PEDIDO a
// seller comes from the sellers master file, with their full name and a code
// ("andy.almanza", "ANDY ALMANZA PÉREZ"). In Rutas they come from the NAME OF A
// DRIVE FOLDER, which is whatever whoever created the phone's profile felt like
// typing: "ANDY", "Alendy Torres GPS", "STGTadyslai", "TABLET3".
//
// So this matches on names, and the important rule is the one that REFUSES:
//
//	when there is more than one candidate on EITHER side, nothing is paired.
//
// Attributing "ALEXANDER"'s route to the wrong Alexander does not produce an
// incomplete panel, it produces a WRONG one — a seller whose orders show as
// unvisited while somebody else takes the credit, and no way to notice from the
// screen. An unpaired seller, on the other hand, says so out loud and gets fixed
// by hand once.

// Ruido: what a folder is called that says nothing about who it belongs to. They
// are dropped before comparing, because "GPS Diana Acosta" and "Diana Acosta" are
// the same person.
var ruido = map[string]bool{
	"gps": true, "tablet": true, "tab": true, "movil": true, "celular": true,
	"telefono": true, "ruta": true, "rutas": true, "procovar": true,
	"supervisor": true, "supervisora": true, "de": true, "del": true, "la": true,
}

// Prefijos de sucursal pegados al nombre: "STGTadyslai" es Tadyslai, de Santiago.
// Se prueba TAMBIÉN sin el prefijo, nunca en su lugar: si el nombre entero casa, ese
// gana.
var prefijos = []string{"stg", "cam", "hab", "hol", "bay", "gto", "gtm", "tun", "ltu", "ssp", "moa", "ptj", "cmg"}

// Match is a resolved pairing.
type Match struct {
	SellerID   string
	VendorCode string
	VendorName string
}

// SellerRef is a Rutas seller as far as matching is concerned.
type SellerRef struct {
	ID       string
	Name     string
	BranchID string
}

// VendorRef is a PEDIDO vendor.
type VendorRef struct {
	Code     string
	Name     string
	BranchID string
}

// MatchVendors pairs what can be paired WITHOUT DOUBT, and leaves the rest alone.
//
// Doubt is checked in BOTH directions, and that is the whole point:
//
//   - a vendor whose name fits two sellers is not paired;
//   - a seller whom two vendors fit is not paired either.
//
// The second one is the case that gets missed. "ALEXANDER" is a folder, and in
// PEDIDO there are Alexander Rodríguez and Alexander Pérez: taking the first one
// would hand that folder's route to one of them at random, and the other would show
// up with his orders unvisited without ever having done anything wrong.
func MatchVendors(sellers []SellerRef, vendors []VendorRef) []Match {
	// Los candidatos de cada lado. Los índices y no los objetos: lo que interesa es
	// CUÁNTOS hay, y solo cuando hay uno se mira cuál.
	deVendedor := make([][]int, len(vendors))
	deTrabajador := make([][]int, len(sellers))

	for iv, v := range vendors {
		for it, t := range sellers {
			// Sucursales distintas no se estorban: un "ALEXANDER" de Camagüey y otro
			// de Santiago no son una ambigüedad, son dos paneles distintos.
			if t.BranchID != v.BranchID {
				continue
			}
			if casan(t.Name, v.Name, v.Code) {
				deVendedor[iv] = append(deVendedor[iv], it)
				deTrabajador[it] = append(deTrabajador[it], iv)
			}
		}
	}

	out := []Match{}
	for iv, candidatos := range deVendedor {
		if len(candidatos) != 1 {
			continue
		}
		it := candidatos[0]
		if len(deTrabajador[it]) != 1 {
			continue
		}
		out = append(out, Match{
			SellerID:   sellers[it].ID,
			VendorCode: vendors[iv].Code,
			VendorName: vendors[iv].Name,
		})
	}
	return out
}

// casan decides whether a Drive folder's name and a PEDIDO vendor are the same
// person.
func casan(nombreCarpeta, nombreVendedor, codigoVendedor string) bool {
	carpeta := gpx.Normalize(nombreCarpeta)
	vendedor := gpx.Normalize(nombreVendedor)
	codigo := gpx.Normalize(codigoVendedor)

	if carpeta == "" || (vendedor == "" && codigo == "") {
		return false
	}

	// 1. El nombre entero, o el código entero. Es el caso limpio.
	if carpeta == vendedor || carpeta == codigo {
		return true
	}

	// 2. Por palabras: las de la carpeta tienen que estar TODAS en las del vendedor.
	//    "Alendy Torres GPS" cae dentro de "ALENDY TORRES PÉREZ"; "Alexander" cae
	//    dentro de "ALEXANDER RODRÍGUEZ". Al revés no: "Alexander" no puede
	//    quedarse con un vendedor llamado solo "Rodríguez".
	suyas := palabras(nombreCarpeta)
	if len(suyas) == 0 {
		return false
	}
	del := conjunto(append(palabras(nombreVendedor), palabras(codigoVendedor)...))
	todas := true
	for _, p := range suyas {
		if !del[p] {
			todas = false
			break
		}
	}
	if todas {
		return true
	}

	// 3. Un solo trozo pegado, con el prefijo de la sucursal delante: "STGTadyslai".
	//    Solo se intenta cuando la carpeta es UNA palabra: si trae varias, el paso
	//    anterior ya tuvo su oportunidad con más información que esta.
	if len(suyas) == 1 {
		for _, pre := range prefijos {
			resto := strings.TrimPrefix(suyas[0], pre)
			// Menos de cuatro letras no identifica a nadie: "STGari" → "ari"
			// casaría con cualquiera que lleve un "Ariel", un "María"…
			if resto != suyas[0] && len(resto) >= 4 && del[resto] {
				return true
			}
		}
	}

	return false
}

// palabras corta un nombre en trozos comparables y tira lo que no dice quién es.
func palabras(s string) []string {
	crudas := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') &&
			!strings.ContainsRune("áéíóúüñàèìòùâêîôûäëïö", r)
	})

	out := make([]string, 0, len(crudas))
	for _, c := range crudas {
		n := gpx.Normalize(c)
		// Una o dos letras es una inicial, y una inicial casa con media plantilla.
		if n == "" || len(n) <= 2 || ruido[n] {
			continue
		}
		out = append(out, n)
	}
	return out
}

func conjunto(ps []string) map[string]bool {
	m := make(map[string]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}
