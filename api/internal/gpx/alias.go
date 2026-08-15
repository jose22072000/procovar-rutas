package gpx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// De fichero a vendedor, y de fichero a fecha.
//
// Las dos preguntas que hay que responder antes de poder guardar nada, y las
// dos dependen de cómo estén montadas las carpetas de Drive — que hoy todavía
// no sabemos del todo. Por eso la resolución es una CADENA de reglas ordenada,
// con una tabla de alias al final: cuando ninguna acierta, el fichero va a la
// bandeja y un admin lo casa a mano UNA vez; el alias queda memorizado y no
// vuelve a preguntar. Ningún fichero se pierde en silencio.

// SourceType dice qué representa una carpeta de Drive. No se deduce del árbol:
// se declara al dar la carpeta de alta, porque el árbol real varía de una cuenta
// padre a otra.
type SourceType string

const (
	FuenteSucursal SourceType = "SUCURSAL"
	FuenteVendedor SourceType = "VENDEDOR"
	FuenteMixta    SourceType = "MIXTA"
)

// Via indica qué regla acertó, para poder explicarlo en la bandeja y depurar.
type Via string

const (
	ViaFuente  Via = "fuente"
	ViaCarpeta Via = "carpeta"
	ViaFichero Via = "fichero"
	ViaGpx     Via = "gpx"
	ViaNinguna Via = ""
)

// Context es todo lo que se sabe de un fichero al intentar resolverlo.
type Context struct {
	SourceType SourceType
	// SourceSellerID es el dueño de la carpeta entera, si SourceType = VENDEDOR.
	SourceSellerID string
	// SourceName es el nombre de la carpeta dada de alta. En el montaje real
	// de Procovar cada carpeta compartida ES el perfil de GPS de un vendedor
	// ("GPS Diana Acosta", "STGGari"), así que muchas veces es la única pista.
	SourceName string
	// FolderPath son las subcarpetas dentro de la fuente, de fuera a dentro.
	FolderPath []string
	FileName   string
	// GpxHints son los textos sacados del propio fichero.
	GpxHints []string
	// Alias mapea alias normalizado -> ID de trabajador.
	Alias map[string]string
}

// Resolution es el veredicto.
type Resolution struct {
	SellerID string
	Via      Via
	// Pista es el texto por el que se preguntará al admin si no se resolvió.
	Pista string
}

var (
	noAlfanumerico = regexp.MustCompile(`[^a-z0-9]+`)
	separadores    = regexp.MustCompile(`[_\-\s]+`)
	soloFecha      = regexp.MustCompile(`^\d{1,4}([.\-/]\d{1,2}){0,2}$`)
	sufijoGpx      = regexp.MustCompile(`(?i)\.gpx$`)
	fechaISO       = regexp.MustCompile(`(20\d{2})[.\-_/]?(\d{2})[.\-_/]?(\d{2})`)
	fechaDMY       = regexp.MustCompile(`(\d{1,2})[.\-_/](\d{1,2})[.\-_/](20\d{2})`)
)

// tildes cubre lo que aparece en nombres de personas y de sucursales aquí. Se
// resuelve a mano en vez de con golang.org/x/text a propósito: la biblioteca
// estándar basta para el español y el binario se queda sin dependencias.
var tildes = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "â", "a", "ê", "e",
	"î", "i", "ô", "o", "û", "u", "ä", "a", "ë", "e", "ï", "i", "ö", "o",
)

// Normalizar deja minúsculas, sin tildes y sin separadores, para que
// "José Pérez", "jose_perez" y "JOSE-PEREZ" casen entre sí.
func Normalizar(s string) string {
	return noAlfanumerico.ReplaceAllString(tildes.Replace(strings.ToLower(s)), "")
}

// candidatosDeNombre quita la extensión y los trozos de fecha, que estorban al
// casar el nombre contra un alias.
func candidatosDeNombre(nombre string) []string {
	sinExt := sufijoGpx.ReplaceAllString(nombre, "")
	trozos := separadores.Split(sinExt, -1)

	sinFechas := make([]string, 0, len(trozos))
	for _, t := range trozos {
		if t != "" && !soloFecha.MatchString(t) {
			sinFechas = append(sinFechas, t)
		}
	}

	out := []string{sinExt, strings.Join(sinFechas, " ")}
	return append(out, sinFechas...)
}

// ResolverTrabajador aplica la cadena de reglas; la primera que acierta gana.
func ResolverTrabajador(ctx Context) Resolution {
	// 1. La carpeta entera es de un vendedor: no hay nada que deducir.
	if ctx.SourceType == FuenteVendedor && ctx.SourceSellerID != "" {
		return Resolution{SellerID: ctx.SourceSellerID, Via: ViaFuente}
	}

	// 2. Subcarpetas, de la más interna hacia fuera: "Camagüey/Alexander/…" es
	//    de Alexander, no de Camagüey.
	for i := len(ctx.FolderPath) - 1; i >= 0; i-- {
		carpeta := ctx.FolderPath[i]
		if id, ok := ctx.Alias[Normalizar(carpeta)]; ok {
			return Resolution{SellerID: id, Via: ViaCarpeta, Pista: carpeta}
		}
	}

	// 3. Nombre de la propia carpeta dada de alta.
	if ctx.SourceName != "" {
		if id, ok := ctx.Alias[Normalizar(ctx.SourceName)]; ok {
			return Resolution{SellerID: id, Via: ViaCarpeta, Pista: ctx.SourceName}
		}
	}

	// 4. Nombre del fichero.
	for _, cand := range candidatosDeNombre(ctx.FileName) {
		if cand == "" {
			continue
		}
		if id, ok := ctx.Alias[Normalizar(cand)]; ok {
			return Resolution{SellerID: id, Via: ViaFichero, Pista: cand}
		}
	}

	// 5. Content del GPX: muchos loggers meten ahí el nombre del dispositivo.
	for _, pista := range ctx.GpxHints {
		if id, ok := ctx.Alias[Normalizar(pista)]; ok {
			return Resolution{SellerID: id, Via: ViaGpx, Pista: pista}
		}
	}

	// Sin suerte: a la bandeja, con el texto más informativo que tengamos para
	// que el admin vea QUÉ hay que casar.
	pista := ctx.FileName
	if len(ctx.GpxHints) > 0 {
		pista = ctx.GpxHints[0]
	}
	if ctx.SourceName != "" {
		pista = ctx.SourceName
	}
	if len(ctx.FolderPath) > 0 {
		pista = ctx.FolderPath[len(ctx.FolderPath)-1]
	}
	return Resolution{Via: ViaNinguna, Pista: pista}
}

// FechaDelNombre saca la fecha de nombres como "RUTA_2026-08-15.gpx",
// "20260815_alexander.gpx" o "15-08-2026.gpx". Devuelve "" si no hay ninguna:
// más vale un fichero en la bandeja que una fecha inventada.
func FechaDelNombre(nombre string) string {
	t := sufijoGpx.ReplaceAllString(nombre, "")

	if m := fechaISO.FindStringSubmatch(t); m != nil {
		mes, _ := strconv.Atoi(m[2])
		dia, _ := strconv.Atoi(m[3])
		if mes >= 1 && mes <= 12 && dia >= 1 && dia <= 31 {
			return fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
		}
	}

	// Día primero, que es como se escribe aquí.
	if m := fechaDMY.FindStringSubmatch(t); m != nil {
		dia, _ := strconv.Atoi(m[1])
		mes, _ := strconv.Atoi(m[2])
		if mes >= 1 && mes <= 12 && dia >= 1 && dia <= 31 {
			return fmt.Sprintf("%s-%02d-%02d", m[3], mes, dia)
		}
	}

	return ""
}
