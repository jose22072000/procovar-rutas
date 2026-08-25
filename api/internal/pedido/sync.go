package pedido

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/procovar/procovar-rutas/api/internal/events"
	"github.com/procovar/procovar-rutas/api/internal/gpx"
	"github.com/procovar/procovar-rutas/api/internal/metrics"
	"github.com/procovar/procovar-rutas/api/internal/store"
)

// Service brings PEDIDO's orders over and crosses them with the routes.
//
// Two steps that are kept apart on purpose:
//
//	SYNC   copies orders and clients into the local mirror.
//	CRUCE  measures each order's client against that day's stops.
//
// Apart because the cross has to be re-runnable WITHOUT syncing: when somebody
// fixes a wrong pairing, or the visit radius is retuned, what changes is the
// verdict, not the data. Re-downloading a month of orders to recompute a distance
// would be asking PEDIDO for something that is already here.
type Service struct {
	q    *store.Queries
	pool *pgxpool.Pool
	cli  *Client
	// cola puede ser nil: sin Redis el trabajador sincroniza igual, día a día y con
	// la misma pausa, sólo que sin repartirse entre procesos.
	cola *Cola
	bus  *events.Bus
	log  *slog.Logger
	// Días hacia atrás que se sincronizan en cada pasada.
	ventanaDias int
	// Lo que se espera entre un día y el siguiente. Es lo que evita las ráfagas
	// contra PEDIDO, que es de quien dependen las sucursales para trabajar.
	pausa time.Duration
	// Cuántos días atrasados se encolan por pasada. El histórico son dos mil días y
	// se traen por tandas: no hay ninguna prisa, son días que ya pasaron.
	porTanda int
}

func NewService(
	pool *pgxpool.Pool,
	cli *Client,
	cola *Cola,
	bus *events.Bus,
	log *slog.Logger,
	ventanaDias int,
	pausa time.Duration,
) *Service {
	if ventanaDias <= 0 {
		ventanaDias = 21
	}
	if pausa <= 0 {
		pausa = PausaPorDefecto
	}
	return &Service{
		q:           store.New(pool),
		pool:        pool,
		cli:         cli,
		cola:        cola,
		bus:         bus,
		log:         log,
		ventanaDias: ventanaDias,
		pausa:       pausa,
		porTanda:    DiasPorTanda,
	}
}

// iso es el formato de fecha que se habla con PEDIDO y con la cola.
const iso = "2006-01-02"

// Configured says whether there is anywhere to talk to. Without PEDIDO the panel
// works exactly as before: it just does not paint the orders column.
func (s *Service) Configured() bool { return s != nil && s.cli != nil }

// Result is what one pass did.
type Result struct {
	Clients int `json:"clientes"`
	Orders  int `json:"pedidos"`
	Crosses int `json:"cruces"`
	Linked  int `json:"emparejados"`
	// Traídos es lo que contestó PEDIDO; Clients y Orders, lo que hubo que escribir.
	// Cuando el segundo es mucho menor que el primero, el espejo está haciendo su
	// trabajo.
	Fetched int `json:"traidos"`
}

// Tipos de pasada.
const (
	// TipoIncremental pide sólo lo que se movió desde la última vez.
	TipoIncremental = "incremental"
	// TipoCompleto repasa la ventana entera. Es la red de seguridad.
	TipoCompleto = "completo"
)

// SyncRango trae los pedidos de un rango de días, empareja lo que puede y vuelve a
// cruzar. Quien lo llama es el TRABAJADOR, y lo llama con un solo día: es la unidad
// que hace que ninguna consulta a PEDIDO sea grande.
//
// # Por qué no se baja todo cada vez
//
// La ventana son tres semanas y una sucursal mueve miles de pedidos: bajárselos
// enteros cada hora es pedirle a PEDIDO lo mismo veinticuatro veces al día para
// reescribir, idénticas, unas filas que nadie tocó. Y los CLIENTES son peor, porque
// un cliente no se mueve: su pin es el mismo hoy que hace seis meses.
//
// Así que dos cosas:
//
//   - la pasada incremental le pide a PEDIDO sólo lo que cambió desde el último
//     pedido que tenemos (`?since=`), que casi siempre es nada o cuatro filas;
//   - y el pin de un cliente que ya está guardado, en el mismo sitio y con el mismo
//     nombre, no se vuelve a escribir.
//
// El repaso COMPLETO sigue existiendo y es el que garantiza que no falte nada: el
// incremental se guía por el reloj de PEDIDO, y si allí se corrige una fila sin
// tocar su `updatedAt`, el incremental no se entera. Una vez al día basta.
func (s *Service) SyncRango(ctx context.Context, desde, hasta time.Time, tipo string) (Result, error) {
	var res Result

	if !s.Configured() {
		return res, fmt.Errorf("PEDIDO no está configurado: falta PEDIDO_API_URL")
	}

	registro, err := s.q.OpenOrderSync(ctx, store.OpenOrderSyncParams{ID: newID(), Type: tipo})
	if err != nil {
		return res, err
	}
	cerrar := func(ok bool, detalle string) {
		_ = s.q.CloseOrderSync(ctx, store.CloseOrderSyncParams{
			ID:      registro.ID,
			Clients: int32(res.Clients),
			Orders:  int32(res.Orders),
			Crosses: int32(res.Crosses),
			Ok:      ok,
			Detail:  opcional(detalle),
		})
	}

	// El cursor sale del propio espejo, no de un contador aparte: un contador se
	// desincroniza del dato el día que algo falle a medias, y entonces deja de
	// traerse lo que falta sin que nadie se entere.
	var since *time.Time
	if tipo == TipoIncremental {
		if c, err := s.q.LastOrderCursor(ctx); err == nil && c.Year() > 1971 {
			// Un minuto de solapamiento: `since` es estrictamente mayor, y dos
			// pedidos guardados en el mismo segundo se perderían si se pide justo
			// desde el último.
			c = c.Add(-time.Minute)
			since = &c
		}
	}

	pedidos, err := s.cli.Orders(ctx, desde, hasta, since)
	if err != nil {
		cerrar(false, err.Error())
		return res, err
	}
	res.Fetched = len(pedidos)

	// Las sucursales resueltas una vez por pasada: los 8.000 pedidos de Camagüey
	// traen todos la misma, y buscarla por cada uno son 8.000 consultas para saber
	// lo mismo ocho mil veces.
	sucursales := map[string]string{}
	// Y los pines que ya están guardados, para no reescribir el que no se ha movido.
	pines := s.pinesGuardados(ctx, pedidos, sucursales)

	for _, p := range pedidos {
		if p.Client == nil || p.Client.Lat == nil || p.Client.Lon == nil {
			// PEDIDO ya filtra los que no tienen geo; si alguno se cuela, se salta
			// sin ruido: sin coordenadas no hay nada que medir.
			continue
		}

		sucursalID, err := s.sucursalDe(ctx, p, sucursales)
		if err != nil {
			s.log.Warn("pedido sin sucursal reconocible", "pedido", p.ID, "error", err)
			continue
		}

		fecha, err := fechaDe(p.Date)
		if err != nil {
			s.log.Warn("pedido con fecha ilegible", "pedido", p.ID, "fecha", p.Date)
			continue
		}

		clienteID := idDe(sucursalID, p.Client.ID)

		// El pin, sólo si es nuevo o si se movió. Un cliente que ya está guardado en
		// el mismo sitio y con el mismo nombre no se vuelve a escribir: es el caso
		// normal, y son miles de filas por pasada.
		if pinCambio(pines[sucursalID+":"+p.Client.ID], p.Client) {
			if err := s.q.UpsertClient(ctx, store.UpsertClientParams{
				ID:           clienteID,
				BranchID:     sucursalID,
				Ref:          p.Client.ID,
				Code:         p.Client.Code,
				Name:         p.Client.Name,
				Address:      p.Client.Address,
				Municipality: p.Client.Municipality,
				Zone:         p.Client.Zone,
				Lat:          *p.Client.Lat,
				Lon:          *p.Client.Lon,
			}); err != nil {
				return res, fmt.Errorf("guardando cliente %s: %w", p.Client.ID, err)
			}
			// Y se apunta ya como guardado: dos pedidos del mismo cliente en la misma
			// tanda no lo escriben dos veces.
			pines[sucursalID+":"+p.Client.ID] = &store.ClientPinsRow{
				Ref: p.Client.ID, Name: p.Client.Name, Lat: *p.Client.Lat, Lon: *p.Client.Lon,
			}
			res.Clients++
		}

		var vendedorRef, vendedorCodigo, vendedorNombre *string
		if p.Vendor != nil {
			vendedorRef = opcional(p.Vendor.ID)
			vendedorCodigo = opcional(p.Vendor.Code)
			vendedorNombre = opcional(p.Vendor.Name)
		}

		if err := s.q.UpsertOrder(ctx, store.UpsertOrderParams{
			ID:              idDe(sucursalID, p.ID),
			BranchID:        sucursalID,
			Ref:             p.ID,
			Folio:           p.Folio,
			Date:            fecha,
			ClientID:        &clienteID,
			VendorRef:       vendedorRef,
			VendorCode:      vendedorCodigo,
			VendorName:      vendedorNombre,
			Status:          p.Status,
			NeedsDelivery:   p.NeedsDelivery,
			SourceUpdatedAt: p.UpdatedAt,
		}); err != nil {
			return res, fmt.Errorf("guardando pedido %s: %w", p.ID, err)
		}
		res.Orders++
	}

	// Emparejar y atar: primero se decide quién es quién, después se ata cada
	// pedido a su trabajador. En este orden, un pedido que llegó antes de que
	// existiera el emparejamiento se ata igual.
	emparejados, err := s.MatchAndLink(ctx)
	if err != nil {
		cerrar(false, err.Error())
		return res, err
	}
	res.Linked = emparejados

	cruces, err := s.CrossRange(ctx, desde, hasta)
	if err != nil {
		cerrar(false, err.Error())
		return res, err
	}
	res.Crosses = cruces

	cerrar(true, "")
	s.avisar()
	return res, nil
}

// SyncVendors trae el maestro de vendedores de PEDIDO y lo espeja.
//
// Es barato —son decenas de filas, no miles— y sin él la pantalla de emparejar solo
// conoce a los vendedores que ya trajeron algún pedido: con el histórico todavía en
// la cola, eso son cuatro de treinta, y quien está emparejando se queda esperando a
// que aparezcan los demás.
func (s *Service) SyncVendors(ctx context.Context) (int, error) {
	if !s.Configured() {
		return 0, fmt.Errorf("PEDIDO no está configurado: falta PEDIDO_API_URL")
	}

	vendedores, err := s.cli.Vendors(ctx)
	if err != nil {
		return 0, err
	}

	sucursales := map[string]string{}
	n := 0
	for _, v := range vendedores {
		// La sucursal, por el mismo camino que los pedidos: por nombre normalizado.
		// Si no se reconoce, el vendedor se guarda IGUAL pero sin sucursal — que no
		// se sepa de qué sucursal es no es razón para esconderlo de la lista de
		// emparejar; es una razón más para enseñarlo.
		var sucursalID *string
		if id, err := s.sucursalDe(ctx, Order{
			BranchCode: v.BranchCode, BranchName: v.BranchName,
		}, sucursales); err == nil {
			sucursalID = &id
		}

		if err := s.q.UpsertVendor(ctx, store.UpsertVendorParams{
			ID:       idDe("vendedor", v.ID),
			BranchID: sucursalID,
			Ref:      v.ID,
			Code:     v.Code,
			Name:     v.Name,
			Active:   v.Active,
			Orders:   int32(v.Orders),
		}); err != nil {
			return n, fmt.Errorf("guardando vendedor %s: %w", v.ID, err)
		}
		n++
	}
	return n, nil
}

// MatchAndLink pairs vendors with sellers and ties each order to its worker.
// Returns how many pairings there are in total.
func (s *Service) MatchAndLink(ctx context.Context) (int, error) {
	trabajadores, err := s.q.SellersForMatch(ctx)
	if err != nil {
		return 0, err
	}
	sellers := make([]SellerRef, 0, len(trabajadores))
	for _, t := range trabajadores {
		sellers = append(sellers, SellerRef{ID: t.ID, Name: t.Name, BranchID: t.BranchID})
	}

	// Los vendedores del MAESTRO que aún no tienen dueño.
	//
	// Antes esto miraba los pedidos huérfanos, y eso encadenaba dos esperas: un
	// vendedor no se podía emparejar hasta que llegara alguno de sus pedidos, y sus
	// pedidos no se cruzaban hasta que estuviera emparejado. Con el maestro espejado
	// se empareja en cuanto EXISTE, y sus pedidos entran ya con dueño.
	todos, err := s.q.VendorsWithLink(ctx, "")
	if err != nil {
		return 0, err
	}
	vendors := make([]VendorRef, 0, len(todos))
	for _, v := range todos {
		if v.SellerID != "" {
			continue // ya tiene dueño
		}
		codigo := v.Ref
		if v.Code != nil && *v.Code != "" {
			codigo = *v.Code
		}
		if v.BranchID == nil {
			continue // sin sucursal no se puede comparar contra nadie
		}
		vendors = append(vendors, VendorRef{
			Code:     codigo,
			Name:     v.Name,
			BranchID: *v.BranchID,
		})
	}

	parejas := MatchVendors(sellers, vendors)
	for _, m := range parejas {
		// La sucursal sale del trabajador emparejado, no del vendedor: es la misma
		// (el emparejamiento exige que coincidan) y así la fila no depende de que
		// PEDIDO tenga bien puesta la suya.
		var sucursalID string
		for _, t := range sellers {
			if t.ID == m.SellerID {
				sucursalID = t.BranchID
				break
			}
		}
		if err := s.q.UpsertSellerLink(ctx, store.UpsertSellerLinkParams{
			ID:         idDe(sucursalID, m.VendorCode),
			BranchID:   sucursalID,
			SellerID:   m.SellerID,
			VendorCode: m.VendorCode,
			VendorName: m.VendorName,
			Origin:     "auto",
		}); err != nil {
			return 0, err
		}
	}

	if _, err := s.q.LinkOrdersToSellers(ctx); err != nil {
		return 0, err
	}
	return len(parejas), nil
}

// CrossRange re-crosses every day in the range that has orders.
func (s *Service) CrossRange(ctx context.Context, desde, hasta time.Time) (int, error) {
	// Primero, que el día exista. Un pedido de HOY no tiene todavía fila de día —la
	// crea el fichero al terminar la jornada— y sin fila no hay dónde colgar la
	// visita: los pedidos de hoy no se verían hasta mañana.
	if _, err := s.q.EnsureDaysForOrders(ctx, store.EnsureDaysForOrdersParams{
		FromDate: desde, ToDate: hasta,
	}); err != nil {
		return 0, err
	}

	dias, err := s.q.DaysToCross(ctx, store.DaysToCrossParams{FromDate: desde, ToDate: hasta})
	if err != nil {
		return 0, err
	}

	// El radio es por sucursal y se lee una vez por sucursal, no por día.
	radios := map[string]float64{}

	n := 0
	for _, d := range dias {
		radio, ok := radios[d.BranchID]
		if !ok {
			radio = s.radioVisita(ctx, d.BranchID)
			radios[d.BranchID] = radio
		}
		if err := s.CrossDay(ctx, d.ID, d.SellerID, d.Date, radio); err != nil {
			s.log.Warn("no se pudo cruzar el día", "dia", d.ID, "error", err)
			continue
		}
		n++
	}
	return n, nil
}

// CrossDay measures one day's orders against its stops.
//
// A visit is a stop within `radio` metres of the client's door. The radius is per
// branch (80 m by default) and it is not a whim: a phone's GPS in the street does
// not land on the exact doorway, and in a small town two clients on the same block
// are closer together than that. That is why the DISTANCE is stored as well —
// whoever is looking decides whether 74 m is good enough.
func (s *Service) CrossDay(ctx context.Context, trackDayID, trabajadorID string, fecha time.Time, radio float64) error {
	pedidos, err := s.q.OrdersForDay(ctx, store.OrdersForDayParams{SellerID: trabajadorID, Date: fecha})
	if err != nil {
		return err
	}

	// Se borra siempre, incluso sin pedidos: si el pedido de ayer se anuló, su
	// visita no puede seguir contando en el calendario de ayer.
	if err := s.q.DeleteDayVisits(ctx, trackDayID); err != nil {
		return err
	}
	if err := s.q.ClearDayStopClients(ctx, trackDayID); err != nil {
		return err
	}
	if len(pedidos) == 0 {
		return nil
	}

	paradas, err := s.q.StopsForCross(ctx, trackDayID)
	if err != nil {
		return err
	}

	// Cada parada se queda con el cliente más cercano que tenga dentro del radio, y
	// cada pedido con la parada más cercana. No es lo mismo mirado dos veces: dos
	// pedidos del mismo portal comparten parada, y una parada larga en un almacén
	// no es de nadie.
	mejorDeParada := make([]int, len(paradas)) // índice del pedido más cercano
	distDeParada := make([]float64, len(paradas))
	for i := range mejorDeParada {
		mejorDeParada[i] = -1
		distDeParada[i] = math.MaxFloat64
	}

	for idx, p := range pedidos {
		cliente := metrics.Coord{Lat: p.Lat, Lon: p.Lon}

		mejor := -1
		mejorDist := math.MaxFloat64
		for i, parada := range paradas {
			d := metrics.DistanceM(cliente, metrics.Coord{Lat: parada.Lat, Lon: parada.Lon})
			if d < mejorDist {
				mejorDist, mejor = d, i
			}
		}

		visita := store.CreateVisitParams{
			ID:         idDe(trackDayID, p.OrderID),
			TrackDayID: trackDayID,
			PedidoID:   p.OrderID,
			ClientID:   p.ClientID,
			Visited:    false,
		}

		if mejor >= 0 {
			// La distancia se guarda SIEMPRE, se cuente como visita o no: es lo que
			// distingue "pasó de largo a 200 m" de "no se acercó en todo el día".
			d := mejorDist
			visita.DistanceM = &d

			if mejorDist <= radio {
				parada := paradas[mejor]
				visita.Visited = true
				visita.StopID = &parada.ID
				inicio := parada.Start
				visita.Time = &inicio
				min := parada.DurationMin
				visita.Minutes = &min

				if mejorDist < distDeParada[mejor] {
					distDeParada[mejor] = mejorDist
					mejorDeParada[mejor] = idx
				}
			}
		}

		if err := s.q.CreateVisit(ctx, visita); err != nil {
			return err
		}
	}

	// La parada deja de ser un punto anónimo y pasa a llamarse como su cliente.
	for i, parada := range paradas {
		if mejorDeParada[i] < 0 {
			continue
		}
		p := pedidos[mejorDeParada[i]]
		d := distDeParada[i]
		if err := s.q.SetStopClient(ctx, store.SetStopClientParams{
			ID:          parada.ID,
			ClientRef:   &p.ClientID,
			ClientName:  &p.ClientName,
			ClientDistM: &d,
		}); err != nil {
			return err
		}
	}

	return nil
}

// CrossOneDay re-crosses a single seller's day. It is what ingest calls after
// recomputing a day: new stops, new verdict.
func (s *Service) CrossOneDay(ctx context.Context, trackDayID, trabajadorID, sucursalID string, fecha time.Time) error {
	return s.CrossDay(ctx, trackDayID, trabajadorID, fecha, s.radioVisita(ctx, sucursalID))
}

func (s *Service) radioVisita(ctx context.Context, sucursalID string) float64 {
	const porDefecto = 80.0
	c, err := s.q.BranchConfig(ctx, sucursalID)
	if err != nil {
		return porDefecto
	}
	if c.VisitRadiusM <= 0 {
		return porDefecto
	}
	return float64(c.VisitRadiusM)
}

// sucursalDe resolves which branch an order belongs to.
//
// By NORMALIZED NAME and not by code, because that is the key Rutas' branches are
// identified by: they were born from the name of the Drive account and the same
// one comes written every possible way. PEDIDO's code (CAM, STG) is recorded on
// the row so both applications can be read side by side, but it is not what
// decides.
func (s *Service) sucursalDe(ctx context.Context, p Order, cache map[string]string) (string, error) {
	nombre := ""
	if p.BranchName != nil {
		nombre = *p.BranchName
	}
	codigo := ""
	if p.BranchCode != nil {
		codigo = strings.TrimSpace(*p.BranchCode)
	}

	clave := gpx.Normalize(nombre)
	// El sufijo de la empresa sobra igual que en la migración 000003: en Drive hay
	// cuentas "santiagoprocovar" y aquí puede llegar "SANTIAGO PROCOVAR".
	clave = strings.TrimSuffix(clave, "procovar")
	if clave == "" {
		return "", fmt.Errorf("el pedido no dice de qué sucursal es")
	}

	if id, ok := cache[clave]; ok {
		return id, nil
	}

	suc, err := s.q.BranchByKey(ctx, clave)
	if err != nil {
		// NO se crea la sucursal: las de aquí nacen de Drive, y una inventada desde
		// PEDIDO sería una sucursal sin carpetas, sin vendedores y sin rutas que
		// solo serviría para ensuciar el panel.
		return "", fmt.Errorf("no hay sucursal con clave %q (de %q)", clave, nombre)
	}

	if codigo != "" {
		if err := s.q.SetBranchCode(ctx, store.SetBranchCodeParams{ID: suc.ID, Code: &codigo}); err != nil {
			s.log.Warn("no se pudo guardar el código de sucursal", "sucursal", suc.ID, "error", err)
		}
	}

	cache[clave] = suc.ID
	return suc.ID, nil
}

// avisar le dice a las pantallas abiertas que hay algo nuevo que mirar. Si Redis no
// contesta no pasa nada: es un aviso, no el dato.
func (s *Service) avisar() {
	if s.bus == nil {
		return
	}
	_ = s.bus.Publish(context.Background(), events.Event{Type: events.TypeOrders})
}

// --- los pines ya guardados -------------------------------------------------

// pinesGuardados lee de una vez los clientes que ya están en el espejo, para las
// sucursales que aparecen en esta tanda.
//
// De una vez y no cliente a cliente: preguntar «¿tengo ya a este?» por cada pedido
// son miles de consultas para acabar sabiendo que no había que escribir ninguna. Una
// sucursal son unos 8.000 clientes; en memoria es medio megabyte y dura lo que dura
// la pasada.
//
// Si la lectura falla se devuelve el mapa vacío y todo se reescribe, que es lo de
// antes: esto es una optimización, no una fuente de verdad, y no puede ser la razón
// de que una pasada se caiga.
func (s *Service) pinesGuardados(ctx context.Context, pedidos []Order, cache map[string]string) map[string]*store.ClientPinsRow {
	pines := map[string]*store.ClientPinsRow{}

	vistas := map[string]bool{}
	for _, p := range pedidos {
		sucursalID, err := s.sucursalDe(ctx, p, cache)
		if err != nil || vistas[sucursalID] {
			continue
		}
		vistas[sucursalID] = true

		filas, err := s.q.ClientPins(ctx, sucursalID)
		if err != nil {
			s.log.Warn("no se pudieron leer los pines guardados", "sucursal", sucursalID, "error", err)
			continue
		}
		for i := range filas {
			pines[sucursalID+":"+filas[i].Ref] = &filas[i]
		}
	}
	return pines
}

// pinCambio dice si hay algo que escribir. Sin pin guardado, sí: es nuevo.
//
// Se comparan las coordenadas y el nombre, que es lo que se dibuja en el mapa y lo
// que se lee en la lista. Lo demás —dirección, municipio, zona— viaja en el mismo
// upsert y se refresca cuando alguna de las dos cambie o en el repaso completo; no
// vale la pena reescribir ocho mil filas porque a un cliente le corrigieron el número
// de la calle.
func pinCambio(guardado *store.ClientPinsRow, viene *Client_) bool {
	if guardado == nil {
		return true
	}
	if guardado.Name != viene.Name {
		return true
	}
	// Un grado son ~111 km, así que 1e-7 es un centímetro: por debajo de eso no hay
	// movimiento, hay ruido de redondeo al ir y venir en JSON.
	const nada = 1e-7
	return math.Abs(guardado.Lat-*viene.Lat) > nada || math.Abs(guardado.Lon-*viene.Lon) > nada
}

// --- auxiliares -------------------------------------------------------------

// hoy in Havana: the working day is local, and in UTC it spills into the next one.
func hoy() time.Time {
	zona, err := time.LoadLocation("America/Havana")
	if err != nil {
		zona = time.UTC
	}
	n := time.Now().In(zona)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// fechaDe reads PEDIDO's date. It arrives as a full ISO instant and only the day
// is of interest; the day is taken in Havana, which is the one written on the
// order.
func fechaDe(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("vacía")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		zona, err2 := time.LoadLocation("America/Havana")
		if err2 != nil {
			zona = time.UTC
		}
		l := t.In(zona)
		return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("fecha ilegible: %q", s)
}

func idDe(partes ...string) string {
	suma := sha256.Sum256([]byte(strings.Join(partes, ":")))
	return hex.EncodeToString(suma[:16])
}

func newID() string {
	return idDe(time.Now().UTC().Format(time.RFC3339Nano))
}

func opcional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
