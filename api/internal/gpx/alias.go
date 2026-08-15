package gpx

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// From file to seller, and from file to date.
//
// The two questions that have to be answered before anything can be stored, and
// both depend on how the Drive folders are laid out — which we still do not fully
// know. That is why resolution is an ordered CHAIN of rules with an alias table at
// the end: when none of them hits, the file goes to the inbox and an admin matches
// it by hand ONCE; the alias is remembered and never asked again. No file is lost
// in silence.

// SourceType says what a Drive folder represents. It is not inferred from the
// tree: it is declared when the folder is registered, because the real tree varies
// from one parent account to another.
type SourceType string

const (
	SourceBranch SourceType = "SUCURSAL"
	SourceSeller SourceType = "VENDEDOR"
	SourceMixed  SourceType = "MIXTA"
)

// Via records which rule hit, so it can be explained in the inbox and debugged.
type Via string

const (
	ViaFuente  Via = "fuente"
	ViaCarpeta Via = "carpeta"
	ViaFichero Via = "fichero"
	ViaGpx     Via = "gpx"
	ViaNinguna Via = ""
)

// Context is everything known about a file when trying to resolve it.
type Context struct {
	SourceType SourceType
	// SourceSellerID owns the entire folder, when SourceType = VENDEDOR.
	SourceSellerID string
	// SourceName is the name of the registered folder. In Procovar's real setup
	// each shared folder IS a seller's GPS profile ("GPS Diana Acosta", "STGGari"),
	// so quite often it is the only hint there is.
	SourceName string
	// FolderPath is the sub-folders inside the source, outermost first.
	FolderPath []string
	FileName   string
	// GpxHints are the texts pulled from the file itself.
	GpxHints []string
	// Alias maps a normalized alias -> seller id.
	Alias map[string]string
}

// Resolution is the verdict.
type Resolution struct {
	SellerID string
	Via      Via
	// Hint is the text the admin will be asked about if it could not be resolved.
	Hint string
}

var (
	noAlfanumerico = regexp.MustCompile(`[^a-z0-9]+`)
	separadores    = regexp.MustCompile(`[_\-\s]+`)
	soloFecha      = regexp.MustCompile(`^\d{1,4}([.\-/]\d{1,2}){0,2}$`)
	sufijoGpx      = regexp.MustCompile(`(?i)\.gpx$`)
	fechaISO       = regexp.MustCompile(`(20\d{2})[.\-_/]?(\d{2})[.\-_/]?(\d{2})`)
	fechaDMY       = regexp.MustCompile(`(\d{1,2})[.\-_/](\d{1,2})[.\-_/](20\d{2})`)
)

// accents covers what shows up in people's and branches' names here. Done by hand
// rather than with golang.org/x/text on purpose: the standard library is enough
// for Spanish and the binary keeps zero dependencies.
var accents = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "â", "a", "ê", "e",
	"î", "i", "ô", "o", "û", "u", "ä", "a", "ë", "e", "ï", "i", "ö", "o",
)

// Normalize lowercases, strips accents and drops separators, so that
// "José Pérez", "jose_perez" y "JOSE-PEREZ" casen entre sí.
func Normalize(s string) string {
	return noAlfanumerico.ReplaceAllString(accents.Replace(strings.ToLower(s)), "")
}

// nameCandidates strips the extension and the date fragments, which get in the
// way of matching a name against an alias.
func nameCandidates(nombre string) []string {
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

// ResolveSeller applies the chain of rules; the first one that hits wins.
func ResolveSeller(ctx Context) Resolution {
	// 1. The whole folder belongs to one seller: there is nothing to infer.
	if ctx.SourceType == SourceSeller && ctx.SourceSellerID != "" {
		return Resolution{SellerID: ctx.SourceSellerID, Via: ViaFuente}
	}

	// 2. Sub-folders, innermost outwards: "Camagüey/Alexander/…" is
	//    Alexander's, not Camagüey's.
	for i := len(ctx.FolderPath) - 1; i >= 0; i-- {
		carpeta := ctx.FolderPath[i]
		if id, ok := ctx.Alias[Normalize(carpeta)]; ok {
			return Resolution{SellerID: id, Via: ViaCarpeta, Hint: carpeta}
		}
	}

	// 3. The name of the registered folder itself.
	if ctx.SourceName != "" {
		if id, ok := ctx.Alias[Normalize(ctx.SourceName)]; ok {
			return Resolution{SellerID: id, Via: ViaCarpeta, Hint: ctx.SourceName}
		}
	}

	// 4. The file name.
	for _, cand := range nameCandidates(ctx.FileName) {
		if cand == "" {
			continue
		}
		if id, ok := ctx.Alias[Normalize(cand)]; ok {
			return Resolution{SellerID: id, Via: ViaFichero, Hint: cand}
		}
	}

	// 5. GPX content: many loggers put the device name in there.
	for _, pista := range ctx.GpxHints {
		if id, ok := ctx.Alias[Normalize(pista)]; ok {
			return Resolution{SellerID: id, Via: ViaGpx, Hint: pista}
		}
	}

	// No luck: to the inbox, with the most informative text we have so
	// the admin can see WHAT needs matching.
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
	return Resolution{Via: ViaNinguna, Hint: pista}
}

// DateFromName pulls the date out of names like "RUTA_2026-08-15.gpx",
// "20260815_alexander.gpx" or "15-08-2026.gpx". Returns "" when there is none:
// better a file in the inbox than an invented date.
func DateFromName(nombre string) string {
	t := sufijoGpx.ReplaceAllString(nombre, "")

	if m := fechaISO.FindStringSubmatch(t); m != nil {
		mes, _ := strconv.Atoi(m[2])
		dia, _ := strconv.Atoi(m[3])
		if mes >= 1 && mes <= 12 && dia >= 1 && dia <= 31 {
			return fmt.Sprintf("%s-%s-%s", m[1], m[2], m[3])
		}
	}

	// Day first, which is how it is written here.
	if m := fechaDMY.FindStringSubmatch(t); m != nil {
		dia, _ := strconv.Atoi(m[1])
		mes, _ := strconv.Atoi(m[2])
		if mes >= 1 && mes <= 12 && dia >= 1 && dia <= 31 {
			return fmt.Sprintf("%s-%02d-%02d", m[3], mes, dia)
		}
	}

	return ""
}
