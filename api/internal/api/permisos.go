package api

import "net/http"

// Las llaves que reparte procovar-auth para esta aplicación.
//
// Aquí no se decide quién puede qué: eso se reparte en Accesos, en una sola
// pantalla, para las seis aplicaciones a la vez. Esto solo nombra las llaves y las
// exige —en la API, no solo en la pantalla—: una función que desaparece del menú
// pero sigue contestando por su URL no está quitada, está escondida.
const (
	PermEntrar         = "rutas.entrar"
	PermCalendario     = "rutas.calendario"
	PermVisor          = "rutas.visor"
	PermReporte        = "rutas.reporte"
	PermBandeja        = "rutas.bandeja"
	PermAdministracion = "rutas.administracion"
	PermCarpeta        = "rutas.carpeta"
	PermAlias          = "rutas.alias"
	PermBarrido        = "rutas.barrido"
)

// Todas, para poder decirle a la pantalla qué tiene esta persona y que esconda lo
// que no.
var TodasLasLlaves = []string{
	PermEntrar, PermCalendario, PermVisor, PermReporte, PermBandeja,
	PermAdministracion, PermCarpeta, PermAlias, PermBarrido,
}

// Exige envuelve un manejador con la llave que hace falta para usarlo.
func Exige(clave string) func(http.Handler) http.Handler {
	return func(siguiente http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := FromContext(r)
			if c == nil || !c.Puede(clave) {
				// 403 y la llave que falta: quien lo lea en el registro sabe qué
				// marcarle a esa persona en Accesos, en vez de adivinar.
				respondError(w, http.StatusForbidden, "sin permiso: "+clave)
				return
			}
			siguiente.ServeHTTP(w, r)
		})
	}
}
