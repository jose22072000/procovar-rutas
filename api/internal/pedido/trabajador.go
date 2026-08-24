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

	dias := s.ventanaDias
	if !completo {
		// Tres días hacia atrás: un pedido de anteayer todavía puede cambiar de
		// estado, uno de hace dos semanas ya no.
		dias = 3
	}

	hasta := hoy()
	encolados := 0
	for i := 0; i <= dias; i++ {
		fecha := hasta.AddDate(0, 0, -i).Format(iso)
		t := Trabajo{Fecha: fecha, Completo: completo}

		if s.cola == nil {
			// Sin Redis no hay dónde encolar: se hace aquí mismo, con la misma pausa,
			// que es lo que de verdad importa. Se pierde el repartirse entre procesos
			// y el sobrevivir a un reinicio, no el ir despacio.
			if err := s.hacerDia(ctx, t); err != nil {
				return encolados, err
			}
			encolados++
			if err := esperar(ctx, s.pausa); err != nil {
				return encolados, err
			}
			continue
		}

		nuevo, err := s.cola.Encolar(ctx, t)
		if err != nil {
			return encolados, err
		}
		if nuevo {
			encolados++
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
