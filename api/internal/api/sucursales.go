package api

import "strings"

// Cruzar la sucursal de Accesos con la de aquí.
//
// Las de aquí nacieron del nombre de la cuenta de Drive y las de Accesos se dieron de
// alta a mano, así que la misma sucursal tiene dos nombres y nada que los una. Se
// cruzan por la clave —sin tildes, sin espacios, en minúsculas— la primera vez que
// entra alguien, y al encontrarla se atan por identificador para no volver a
// adivinar.
func claveDeSucursal(nombre string) string {
	sustituye := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
		"Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u", "Ü", "u", "Ñ", "n",
	)
	var b strings.Builder
	for _, r := range sustituye.Replace(strings.ToLower(strings.TrimSpace(nombre))) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	clave := strings.TrimSuffix(b.String(), "procovar")
	if clave == "" {
		clave = b.String()
	}
	if otra, hay := mismaSucursal[clave]; hay {
		return otra
	}
	return clave
}

// La misma sucursal, escrita de dos maneras.
//
// Granma y Bayamo son la misma: en Accesos es la provincia y en Drive la ciudad donde
// está la oficina, porque la cuenta se llamó así. No es un caso general que se pueda
// deducir de los nombres —Bayamo no "contiene" a Granma en ningún sentido que un
// programa pueda ver—, es un hecho de la empresa y por eso está escrito.
//
// Si aparece otra pareja así, se añade aquí una línea.
var mismaSucursal = map[string]string{
	"granma": "bayamo",
}
