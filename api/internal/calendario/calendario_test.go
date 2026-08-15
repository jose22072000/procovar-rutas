package calendario

import (
	"testing"
	"time"
)

func dia(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestSemanaLaboralEsLunesAViernes(t *testing.T) {
	// El 13 de agosto de 2026 es jueves.
	semana := SemanaLaboral(dia("2026-08-13"))

	if len(semana) != 5 {
		t.Fatalf("días = %d, la semana del panel es L–V", len(semana))
	}
	if semana[0].Format("2006-01-02") != "2026-08-10" {
		t.Errorf("empieza en %s, se esperaba el lunes 10", semana[0].Format("2006-01-02"))
	}
	if semana[4].Format("2006-01-02") != "2026-08-14" {
		t.Errorf("termina en %s, se esperaba el viernes 14", semana[4].Format("2006-01-02"))
	}
}

// El domingo es el caso que rompe las implementaciones ingenuas: en Go es el
// día 0, así que sin convertirlo a ISO la semana se calcularía al revés.
func TestSemanaLaboralDesdeUnDomingo(t *testing.T) {
	semana := SemanaLaboral(dia("2026-08-16")) // domingo

	if semana[0].Format("2006-01-02") != "2026-08-10" {
		t.Errorf("el domingo 16 pertenece a la semana del lunes 10, no a la siguiente: %s",
			semana[0].Format("2006-01-02"))
	}
}

func TestFinDeSemanaNoEsLaborable(t *testing.T) {
	if EsLaborable(dia("2026-08-15"), nil, nil) { // sábado
		t.Error("el sábado no cuenta para el control")
	}
	if EsLaborable(dia("2026-08-16"), nil, nil) { // domingo
		t.Error("el domingo no cuenta")
	}
	if !EsLaborable(dia("2026-08-14"), nil, nil) { // viernes
		t.Error("el viernes sí cuenta")
	}
}

// Sin feriados, un 1 de mayo saldría como ausencia de toda la plantilla.
func TestFeriadoNoEsAusencia(t *testing.T) {
	feriados := map[string]bool{"2026-05-01": true}
	if EsLaborable(dia("2026-05-01"), nil, feriados) {
		t.Error("un feriado no es día laborable")
	}
}

func TestDiasLaborablesDeUnaSemana(t *testing.T) {
	dias := DiasLaborables(dia("2026-08-10"), dia("2026-08-16"), nil)
	if len(dias) != 5 {
		t.Errorf("= %v, se esperaban 5 días", dias)
	}
}

func TestDiasEntre(t *testing.T) {
	d := func(s string) time.Time {
		f, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}

	// Un rango corto, que es el caso que motivó esto: pedir tres días sueltos.
	dias := DiasEntre(d("2026-08-12"), d("2026-08-14"))
	if len(dias) != 3 {
		t.Fatalf("esperaba 3 días, salieron %d", len(dias))
	}

	// Un solo día es un rango válido de un elemento, no vacío.
	if got := DiasEntre(d("2026-08-12"), d("2026-08-12")); len(got) != 1 {
		t.Fatalf("un día suelto debería dar 1, dio %d", len(got))
	}

	// Incluye el fin de semana: el reporte no filtra por laborables.
	finde := DiasEntre(d("2026-08-14"), d("2026-08-17"))
	if len(finde) != 4 {
		t.Fatalf("esperaba 4 días con sábado y domingo, salieron %d", len(finde))
	}

	// Al revés no devuelve nada en vez de colgarse iterando.
	if got := DiasEntre(d("2026-08-14"), d("2026-08-12")); got != nil {
		t.Fatalf("rango invertido debería dar nil, dio %v", got)
	}
}
