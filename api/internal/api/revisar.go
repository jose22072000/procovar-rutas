package api

import (
	"net/http"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

// GET /api/review — todo lo que hay que revisar, en una sola respuesta.
//
// # Por qué esto no vive en el calendario
//
// El calendario es una cuadrícula de vendedores por días laborables y se abre para
// una sola cosa: ver de un vistazo quién trabajó y quién no. Meterle encima la lista
// de los treinta y seis que llevan días sin subir, más los ficheros ilegibles con su
// error de XML, lo convertía en un muro de texto que hay que atravesar para llegar a
// lo que se venía a ver — y encima con el detalle a medias, porque ahí no cabe.
//
// El detalle tiene su sitio, y es este: quién falta, desde cuándo, qué fichero se
// atascó y por qué, para poder ir a subirlo otra vez a mano. El calendario se queda
// con una línea que dice cuántas cosas hay y lleva aquí.
//
// Una sola llamada y no cuatro porque es una sola pantalla: cuatro peticiones para
// pintarla serían cuatro maneras distintas de que salga a medias.
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	c := FromContext(r)

	filtro, err := c.Scope(time.Now())
	if err != nil {
		respondError(w, http.StatusForbidden, err.Error())
		return
	}
	p := fromFilter(filtro)
	if p.Empty {
		respond(w, http.StatusOK, map[string]any{
			"files": []any{}, "silent": []any{}, "links": []any{}, "unlinked": []any{},
		})
		return
	}

	salida := map[string]any{}

	// 1. Los ficheros que llegaron y no se pudieron usar. Es lo que hay que volver a
	//    subir a mano, así que va con TODO su detalle: de qué carpeta salió, de qué
	//    día es y qué dijo exactamente el error.
	ficheros, err := s.q.Inbox(r.Context(), store.InboxParams{
		BranchID: p.BranchID, LimitRows: 200,
	})
	if err != nil {
		s.fail(w, "ficheros atascados", err)
		return
	}
	salida["files"] = aStuckFiles(ficheros)

	// 2. Quién lleva días sin subir, con la fecha real de su última subida — no un
	//    "nunca" que no distingue al que dejó el GPS apagado ayer del que no ha
	//    subido en su vida.
	estados, err := store.UploadStates(r.Context(), s.pool, p.BranchID, p.Sellers, p.Exclude)
	if err != nil {
		s.fail(w, "estado de subida", err)
		return
	}
	nombres, err := s.q.SellersInScope(r.Context(), store.SellersInScopeParams{
		BranchID: p.BranchID, Sellers: p.Sellers, Exclude: p.Exclude,
	})
	if err != nil {
		s.fail(w, "vendedores", err)
		return
	}
	salida["silent"] = aSilentSellers(estados, nombres)

	// 2b. Y los días que entraron A MEDIAS. Es lo que pidió quien usa esto: que no se
	//     pueda leer «8 km» de medio día como si fueran los de la jornada. El fichero
	//     se cortó, sus puntos son buenos, pero acaban donde acaba el fichero.
	cortados, err := s.q.TruncatedDays(r.Context(), store.TruncatedDaysParams{
		BranchID: p.BranchID, LimitRows: 200,
	})
	if err != nil {
		s.log.Warn("días cortados", "error", err)
	} else {
		salida["truncated"] = aTruncatedDays(cortados)
	}

	// 3. Y quién es quién con PEDIDO: los que ya están atados y los que faltan. Los
	//    dos, porque «lo que tengo» es tan informativo como «lo que falta»: sirve
	//    para revisar que un emparejamiento automático no se equivocó.
	if s.pedidos != nil {
		// UNA sola lista, con todos: emparejados y no.
		//
		// Antes eran dos —«los atados» y «los que faltan», esta última deducida de los
		// pedidos huérfanos— y se contradecían en pantalla: veintidós trabajadores
		// marcados «sin emparejar» arriba y «no falta ninguno por emparejar» un palmo
		// más abajo. No eran cuentas mal hechas, eran dos preguntas distintas puestas
		// como si fueran la misma. Ahora se lee el maestro de PEDIDO y cada vendedor
		// sale una vez, con su dueño al lado o con el hueco para decirlo.
		vendedores, err := s.q.VendorsWithLink(r.Context(), p.BranchID)
		if err != nil {
			s.log.Warn("maestro de vendedores", "error", err)
		} else {
			salida["vendors"] = aVendors(vendedores)
		}

		// 4. Y cómo va el trabajador: cuántos días quedan por traer de PEDIDO y
		//    cuántos esperan en la cola. Sin esto, quien mira una pantalla a medio
		//    llenar no sabe si aquello avanza o está parado.
		if faltan, err := s.q.DaysMissingCount(r.Context()); err == nil {
			salida["daysMissing"] = faltan
		}
		if cola, err := s.pedidos.EstadoDeLaCola(r.Context()); err == nil && cola != nil {
			salida["queue"] = cola
		}
	}

	respond(w, http.StatusOK, salida)
}

// StuckFile es un fichero que llegó y no se pudo usar, con lo que hace falta para ir
// a arreglarlo: de qué carpeta salió, de qué día es y qué falló exactamente.
type StuckFile struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Source     string  `json:"source"`
	FolderPath *string `json:"folderPath"`
	Seller     string  `json:"seller"`
	Status     string  `json:"status"`
	Error      *string `json:"error"`
	Date       *string `json:"date"`
	Points     int32   `json:"points"`
	ImportedAt string  `json:"importedAt"`
}

func aStuckFiles(fs []store.InboxRow) []StuckFile {
	out := make([]StuckFile, 0, len(fs))
	for _, f := range fs {
		var fecha *string
		if f.Date != nil {
			d := f.Date.Format(iso)
			fecha = &d
		}
		out = append(out, StuckFile{
			ID:         f.ID,
			Name:       f.Name,
			Source:     f.Source,
			FolderPath: f.FolderPath,
			Seller:     f.Seller,
			Status:     string(f.Status),
			Error:      f.Error,
			Date:       fecha,
			Points:     f.TotalPoints,
			ImportedAt: f.ImportedAt.Format(time.RFC3339),
		})
	}
	return out
}

// SilentSeller es un vendedor y cuándo subió por última vez.
type SilentSeller struct {
	SellerID string `json:"sellerId"`
	Seller   string `json:"seller"`
	Branch   string `json:"branchId"`
	// LastUpload nulo = por ahí no ha entrado nunca ninguna ruta.
	LastUpload *string `json:"lastUpload"`
	DaysSilent int     `json:"daysSilent"`
	StuckFiles int     `json:"stuckFiles"`
	Linked     bool    `json:"linked"`
}

func aSilentSellers(estados []store.UploadState, trabajadores []store.Seller) []SilentSeller {
	nombre := make(map[string]store.Seller, len(trabajadores))
	for _, t := range trabajadores {
		nombre[t.ID] = t
	}

	hoy := time.Now().Truncate(24 * time.Hour)
	out := make([]SilentSeller, 0, len(estados))
	for _, e := range estados {
		t, ok := nombre[e.SellerID]
		if !ok {
			continue
		}
		fila := SilentSeller{
			SellerID:   e.SellerID,
			Seller:     t.Name,
			Branch:     t.BranchID,
			DaysSilent: -1,
			StuckFiles: e.StuckFiles,
			Linked:     e.Linked,
		}
		if e.LastUpload != nil {
			u := e.LastUpload.Format(iso)
			fila.LastUpload = &u
			fila.DaysSilent = int(hoy.Sub(*e.LastUpload).Hours() / 24)
		}
		out = append(out, fila)
	}
	return out
}

// TruncatedDay es un día cuyo fichero llegó cortado.
type TruncatedDay struct {
	SellerID string `json:"sellerId"`
	Seller   string `json:"seller"`
	Date     string `json:"date"`
	File     string `json:"file"`
	Detail   string `json:"detail"`
	Points   int32  `json:"points"`
}

func aTruncatedDays(fs []store.TruncatedDaysRow) []TruncatedDay {
	out := make([]TruncatedDay, 0, len(fs))
	for _, f := range fs {
		out = append(out, TruncatedDay{
			SellerID: f.SellerID,
			Seller:   f.Seller,
			Date:     f.Date.Format(iso),
			File:     f.File,
			Detail:   f.Detail,
			Points:   f.Points,
		})
	}
	return out
}

// Vendor es un vendedor de PEDIDO y quién es aquí, si ya se dijo.
type Vendor struct {
	Ref    string `json:"ref"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Orders int32  `json:"orders"`
	// SellerID vacío = todavía no se sabe quién es aquí, y sus pedidos no se cruzan
	// con ninguna ruta mientras siga así.
	SellerID string `json:"sellerId"`
	Seller   string `json:"seller"`
	// Origin dice quién lo decidió: "auto" el parecido de nombres, "manual" una
	// persona. Sirve para revisar lo que decidió la máquina.
	Origin string `json:"origin"`
}

func aVendors(vs []store.VendorsWithLinkRow) []Vendor {
	out := make([]Vendor, 0, len(vs))
	for _, v := range vs {
		codigo := v.Ref
		if v.Code != nil && *v.Code != "" {
			codigo = *v.Code
		}
		out = append(out, Vendor{
			Ref:      v.Ref,
			Code:     codigo,
			Name:     v.Name,
			Branch:   v.Branch,
			Orders:   v.Orders,
			SellerID: v.SellerID,
			Seller:   v.Seller,
			Origin:   v.Origin,
		})
	}
	return out
}
