package calendar

import (
	"testing"
	"time"
)

func dia(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func TestSemanaLaboralEsLunesAViernes(t *testing.T) {
	// The 13th of August 2026 is a Thursday.
	week := WorkWeek(dia("2026-08-13"))

	if len(week) != 5 {
		t.Fatalf("días = %d, la week del panel es L–V", len(week))
	}
	if week[0].Format("2006-01-02") != "2026-08-10" {
		t.Errorf("empieza en %s, se esperaba el lunes 10", week[0].Format("2006-01-02"))
	}
	if week[4].Format("2006-01-02") != "2026-08-14" {
		t.Errorf("termina en %s, se esperaba el viernes 14", week[4].Format("2006-01-02"))
	}
}

// Sunday is the case that breaks naive implementations: in Go it is day 0, so
// without converting to ISO the week would be computed backwards.
func TestSemanaLaboralDesdeUnDomingo(t *testing.T) {
	week := WorkWeek(dia("2026-08-16")) // domingo

	if week[0].Format("2006-01-02") != "2026-08-10" {
		t.Errorf("el domingo 16 pertenece a la week del lunes 10, no a la siguiente: %s",
			week[0].Format("2006-01-02"))
	}
}

func TestFinDeSemanaNoEsLaborable(t *testing.T) {
	if IsWorkday(dia("2026-08-15"), nil, nil) { // sábado
		t.Error("el sábado no cuenta para el control")
	}
	if IsWorkday(dia("2026-08-16"), nil, nil) { // domingo
		t.Error("el domingo no cuenta")
	}
	if !IsWorkday(dia("2026-08-14"), nil, nil) { // viernes
		t.Error("el viernes sí cuenta")
	}
}

// Without holidays, the 1st of May would show up as the whole workforce absent.
func TestFeriadoNoEsAusencia(t *testing.T) {
	holidays := map[string]bool{"2026-05-01": true}
	if IsWorkday(dia("2026-05-01"), nil, holidays) {
		t.Error("un feriado no es día laborable")
	}
}

func TestDiasLaborablesDeUnaSemana(t *testing.T) {
	days := Workdays(dia("2026-08-10"), dia("2026-08-16"), nil)
	if len(days) != 5 {
		t.Errorf("= %v, se esperaban 5 días", days)
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

	// A short range, the case that prompted this: asking for three separate days.
	days := DaysBetween(d("2026-08-12"), d("2026-08-14"))
	if len(days) != 3 {
		t.Fatalf("esperaba 3 días, salieron %d", len(days))
	}

	// A single day is a valid one-element range, not an empty one.
	if got := DaysBetween(d("2026-08-12"), d("2026-08-12")); len(got) != 1 {
		t.Fatalf("un día suelto debería dar 1, dio %d", len(got))
	}

	// Includes the weekend: the report does not filter by working day.
	finde := DaysBetween(d("2026-08-14"), d("2026-08-17"))
	if len(finde) != 4 {
		t.Fatalf("esperaba 4 días con sábado y domingo, salieron %d", len(finde))
	}

	// Reversed it returns nothing instead of looping for ever.
	if got := DaysBetween(d("2026-08-14"), d("2026-08-12")); got != nil {
		t.Fatalf("rango invertido debería dar nil, dio %v", got)
	}
}
