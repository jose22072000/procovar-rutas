"use client";

/**
 * Un calendario, y sobre él se marca desde dónde hasta dónde.
 *
 * Antes eran un campo de fecha y una lista de tramos ("su semana", "15 días desde
 * ahí"), y para ver del 3 al 11 no había forma: había que elegir el tramo que menos
 * se pasara. Un calendario de verdad enseña el mes entero y el rango se ve mientras
 * se marca, que es como se elige un rango en cualquier sitio.
 *
 * Se toca un día y ese es el desde; se toca otro y ese es el hasta. Si el segundo
 * cae antes que el primero se dan la vuelta, porque nadie quiere un error por haber
 * marcado al revés. Y mientras se busca el segundo, el ratón por encima ya pinta el
 * rango que saldría.
 */

import { useMemo, useState } from "react";

const DIAS = ["Lu", "Ma", "Mi", "Ju", "Vi", "Sá", "Do"];
const MESES = [
  "enero", "febrero", "marzo", "abril", "mayo", "junio",
  "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
];

function iso(d: Date): string {
  // El mediodía evita el clásico "se me fue un día": a las 00:00 cualquier huso
  // negativo tira la fecha al día anterior al pasarla a texto.
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 10);
}

function deISO(s: string): Date {
  const [a, m, d] = s.split("-").map(Number);
  return new Date(a, m - 1, d, 12);
}

// Lunes como primer día: la semana laboral de la empresa es L–V y un calendario que
// empieza en domingo la parte por la mitad.
function primeraCasilla(mes: Date): Date {
  const primero = new Date(mes.getFullYear(), mes.getMonth(), 1, 12);
  const desplazamiento = (primero.getDay() + 6) % 7;
  return new Date(primero.getTime() - desplazamiento * 86400000);
}

export default function RangoFechas({
  desde,
  hasta,
  onCambio,
}: {
  desde: string;
  hasta: string;
  onCambio: (desde: string, hasta: string) => void;
}) {
  const [mes, setMes] = useState(() => deISO(desde));
  // El extremo que se está marcando. Con `null` el próximo toque empieza un rango
  // nuevo; con una fecha, el próximo toque lo cierra.
  const [ancla, setAncla] = useState<string | null>(null);
  const [encima, setEncima] = useState<string | null>(null);

  const casillas = useMemo(() => {
    const inicio = primeraCasilla(mes);
    return Array.from({ length: 42 }, (_, i) =>
      new Date(inicio.getTime() + i * 86400000),
    );
  }, [mes]);

  // Lo que se pintaría ahora mismo: el rango cerrado, o el que va del ancla a donde
  // está el ratón mientras se elige el segundo extremo.
  const [pintaDesde, pintaHasta] = useMemo(() => {
    if (ancla) {
      const otro = encima ?? ancla;
      return ancla <= otro ? [ancla, otro] : [otro, ancla];
    }
    return [desde, hasta];
  }, [ancla, encima, desde, hasta]);

  function tocar(f: string) {
    if (!ancla) {
      setAncla(f);
      return;
    }
    const [d, h] = ancla <= f ? [ancla, f] : [f, ancla];
    setAncla(null);
    setEncima(null);
    onCambio(d, h);
  }

  function moverMes(n: number) {
    setMes(new Date(mes.getFullYear(), mes.getMonth() + n, 1, 12));
  }

  const hoy = iso(new Date());

  return (
    <div className="calendario" onMouseLeave={() => setEncima(null)}>
      <div className="calendario-cabecera">
        <button className="pv-boton" onClick={() => moverMes(-1)} aria-label="Mes anterior">
          ‹
        </button>
        <b>
          {MESES[mes.getMonth()]} {mes.getFullYear()}
        </b>
        <button className="pv-boton" onClick={() => moverMes(1)} aria-label="Mes siguiente">
          ›
        </button>
      </div>

      <div className="calendario-rejilla">
        {DIAS.map((d) => (
          <span key={d} className="calendario-dia">
            {d}
          </span>
        ))}

        {casillas.map((c) => {
          const f = iso(c);
          const deOtroMes = c.getMonth() !== mes.getMonth();
          const dentro = f >= pintaDesde && f <= pintaHasta;
          const extremo = f === pintaDesde || f === pintaHasta;
          return (
            <button
              key={f}
              className="calendario-casilla"
              data-fuera={deOtroMes || undefined}
              data-dentro={dentro || undefined}
              data-extremo={extremo || undefined}
              data-hoy={f === hoy || undefined}
              onClick={() => tocar(f)}
              onMouseEnter={() => ancla && setEncima(f)}
            >
              {c.getDate()}
            </button>
          );
        })}
      </div>

      <p className="calendario-pie">
        {ancla
          ? "Marca el otro extremo"
          : `Del ${deISO(desde).toLocaleDateString("es")} al ${deISO(hasta).toLocaleDateString("es")}`}
      </p>
    </div>
  );
}
