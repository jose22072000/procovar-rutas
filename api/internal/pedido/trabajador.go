package pedido

import (
	"context"
	"fmt"
	"time"

	"github.com/procovar/procovar-rutas/api/internal/store"
)

// El planificador y el trabajador: quién decide qué días hay que refrescar, y quién
// los va haciendo de uno en uno sin agobiar a PEDIDO.
//
// Son dos cosas separadas a propósito. El planificador es barato y puede pasar a
// menudo: mira el calendario y encola lo que falta. El trabajador es el que habla
// con PEDIDO, y va despacio por definición: coge un día, lo hace, ESPERA, coge el
// siguiente. Entre los dos, ponerse al día con un mes atrasado son cuarenta
// consultas chicas repartidas en un rato, no cuarenta de golpe.

// PausaPorDefecto entre un día y el siguiente. Cinco segundos no es un número
// mágico: con la ventana de tres semanas, un repaso completo tarda menos de dos
// minutos y en ese rato PEDIDO ve una consulta pequeña cada cinco segundos, que es
// menos de lo que le hace una tablet sincronizando.
const PausaPorDefecto = 5 * time.Second

// DiasPorTanda: cuántos días atrasados se encolan en cada pasada del planificador.
// Con la pausa de cinco segundos, treinta días son dos minutos y medio de trabajo;
// el histórico entero se pone al día solo en unas cuantas pasadas y sin que nadie lo
// note.
const DiasPorTanda = 30

// Planificar encola los días que hay que refrescar y devuelve cuántos encoló.
//
// Con `completo`, la ventana entera: es el repaso de una vez al día, y el que
// garantiza que no falte nada aunque en PEDIDO corrijan una fila sin tocar su
// `updatedAt`.
//
// Sin `completo`, sólo los últimos días —lo de hoy y lo de anteayer, que es donde se
// mueven los pedidos— y además en modo incremental, así que cada uno de esos días le
// pide a PEDIDO únicamente lo que haya cambiado. Casi siempre, nada.
func (s *Service) Planificar(ctx context.Context, completo bool) (int, error) {
	if !s.Configured() {
		return 0, fmt.Errorf("PEDIDO no está configurado: falta PEDIDO_API_URL")
	}

	encolados := 0
	encolar := func(t Trabajo) error {
		if s.cola == nil {
			// Sin Redis no hay dónde encolar: se hace aquí mismo, con la misma pausa,
			// que es lo que de verdad importa. Se pierde el repartirse entre procesos
			// y el sobrevivir a un reinicio, no el ir despacio.
			if err := s.hacerDia(ctx, t); err != nil {
				return err
			}
			encolados++
			return esperar(ctx, s.pausa)
		}
		nuevo, err := s.cola.Encolar(ctx, t)
		if err != nil {
			return err
		}
		if nuevo {
			encolados++
		}
		return nil
	}

	// 1. LO RECIENTE, siempre. Un pedido de anteayer todavía puede cambiar de estado;
	//    uno de hace dos semanas ya no. Esto se vuelve a pedir aunque ya se haya
	//    traído, porque lo que cambia no es que exista, es lo que dice.
	recientes := 3
	if completo {
		recientes = s.ventanaDias
	}
	hasta := hoy()
	for i := 0; i <= recientes; i++ {
		if err := encolar(Trabajo{Fecha: hasta.AddDate(0, 0, -i).Format(iso), Completo: completo}); err != nil {
			return encolados, err
		}
	}

	// 2. Y LOS DÍAS DE LOS QUE YA HAY RUTA Y NUNCA SE PIDIERON SUS CLIENTES.
	//
	// Aquí está el histórico: dos mil días entraron por el backfill de Drive y de
	// ninguno se había preguntado nunca por dónde tenía que pasar el vendedor. Se
	// abría un día de agosto, salía la ruta dibujada, y no había con qué compararla.
	//
	// Se van trayendo POR TANDAS y de los más recientes hacia atrás: lo que se quiere
	// ver lleno mañana por la mañana es esta semana, no enero. Con la pausa del
	// trabajador, dos mil días son unas cuantas noches — y no hay ninguna prisa,
	// porque son días que ya pasaron.
	faltan, err := s.q.DaysMissingOrders(ctx, int32(s.porTanda))
	if err != nil {
		// Que falle el histórico no puede impedir que se traiga lo de hoy, que es lo
		// que de verdad se está mirando.
		s.log.Warn("no se pudo mirar qué días faltan por traer", "error", err)
		return encolados, nil
	}
	for _, f := range faltan {
		if err := encolar(Trabajo{Fecha: f.Format(iso), Completo: true}); err != nil {
			return encolados, err
		}
	}

	return encolados, nil
}

// Trabajar consume la cola hasta que se le diga que pare. Un día, una pausa, otro
// día.
func (s *Service) Trabajar(ctx context.Context) {
	if s.cola == nil {
		return
	}

	if n, err := s.cola.Recuperar(ctx); err != nil {
		s.log.Warn("no se pudo recuperar la cola de pedidos", "error", err)
	} else if n > 0 {
		s.log.Info("días de pedidos devueltos a la cola tras un reinicio", "dias", n)
	}

	for {
		if ctx.Err() != nil {
			return
		}

		t, crudo, err := s.cola.Tomar(ctx, 5*time.Second)
		if err != nil {
			s.log.Warn("leyendo la cola de pedidos", "error", err)
			if esperar(ctx, s.pausa) != nil {
				return
			}
			continue
		}
		if t == nil {
			continue // no había nada; se vuelve a esperar
		}

		if err := s.hacerDia(ctx, *t); err != nil {
			s.log.Error("no se pudo traer el día", "fecha", t.Fecha, "error", err)
			if err := s.cola.Fallar(ctx, crudo, *t); err != nil {
				s.log.Warn("devolviendo el día a la cola", "error", err)
			}
		} else if err := s.cola.Terminar(ctx, crudo, *t); err != nil {
			s.log.Warn("cerrando el día en la cola", "error", err)
		}

		// LA PAUSA. Es la razón de ser de todo esto: entre un día y el siguiente,
		// PEDIDO respira.
		if esperar(ctx, s.pausa) != nil {
			return
		}
	}
}

// hacerDia trae un día de PEDIDO y lo vuelve a cruzar con el recorrido.
func (s *Service) hacerDia(ctx context.Context, t Trabajo) error {
	fecha, err := time.Parse(iso, t.Fecha)
	if err != nil {
		return fmt.Errorf("fecha ilegible en la cola: %q", t.Fecha)
	}

	tipo := TipoIncremental
	if t.Completo {
		tipo = TipoCompleto
	}

	res, err := s.SyncRango(ctx, fecha, fecha, tipo)
	if err != nil {
		return err
	}

	// Queda apuntado que por este día YA se preguntó, aunque no hubiera ni un pedido.
	// Sin esto, un día sin pedidos es indistinguible de uno que nunca se pidió y se
	// volvería a pedir en cada pasada, para siempre.
	if err := s.q.MarkDayFetched(ctx, store.MarkDayFetchedParams{
		Date: fecha, Orders: int32(res.Orders), Completo: t.Completo,
	}); err != nil {
		s.log.Warn("no se pudo apuntar el día como traído", "fecha", t.Fecha, "error", err)
	}

	// Sólo se avisa a las pantallas cuando hubo algo. Un aviso por cada día vacío
	// haría que el calendario se recargara veinte veces para no cambiar nada.
	if res.Orders > 0 || res.Crosses > 0 {
		s.avisar()
	}
	return nil
}

// esperar duerme, pero se despierta si hay que parar. Un `time.Sleep` pelado
// convierte cinco segundos de pausa en cinco segundos de retraso al apagar el
// contenedor.
func esperar(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// EstadoDeLaCola para poder mirarlo desde fuera. Sin Redis no hay cola que mirar.
func (s *Service) EstadoDeLaCola(ctx context.Context) (*EstadoCola, error) {
	if s == nil || s.cola == nil {
		return nil, nil
	}
	e, err := s.cola.Estado(ctx)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Y la bitácora, que es lo que dice si el trabajador está vivo.
func (s *Service) UltimasPasadas(ctx context.Context, n int32) ([]store.OrderSync, error) {
	return s.q.RecentOrderSyncs(ctx, n)
}
