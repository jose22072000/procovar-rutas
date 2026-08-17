"use client";

/**
 * El rango de fechas: un botón que dice qué se está mirando y, al tocarlo, un
 * calendario para cambiarlo.
 *
 * Antes el calendario estaba siempre desplegado en la barra, ocupando media
 * pantalla por encima de la tabla —que es lo que se viene a ver— para una cosa que
 * se toca una vez y se deja quieta el resto de la mañana. Ahora se abre y se cierra,
 * y cerrado el botón ya dice el rango, así que no hace falta abrirlo para saber
 * qué hay delante.
 *
 * Se toca un día y ese es el desde; se toca otro y ese es el hasta. Si el segundo
 * cae antes que el primero se dan la vuelta, porque nadie quiere un error por haber
 * marcado al revés. Y mientras se busca el segundo, el ratón por encima ya pinta el
 * rango que saldría.
 */

import { useEffect, useMemo, useRef, useState } from "react";

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

function comoSeLee(desde: string, hasta: string): string {
  const d = deISO(desde);
  const h = deISO(hasta);
  const corto = (f: Date) => f.toLocaleDateString("es", { day: "numeric", month: "short" });
  if (desde === hasta) return corto(d);
  // El mismo mes no se repite: "11 – 15 ago" se lee de un golpe.
  if (d.getMonth() === h.getMonth() && d.getFullYear() === h.getFullYear()) {
    return `${d.getDate()} – ${corto(h)}`;
  }
  return `${corto(d)} – ${corto(h)}`;
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
  const [abierto, setAbierto] = useState(false);
  const [mes, setMes] = useState(() => deISO(desde));
  // El extremo que se está marcando. Con `null` el próximo toque empieza un rango
  // nuevo; con una fecha, el próximo toque lo cierra.
  const [ancla, setAncla] = useState<string | null>(null);
  const [encima, setEncima] = useState<string | null>(null);
  const caja = useRef<HTMLDivElement>(null);

  // Cerrar al tocar fuera y con Escape: un panel que se queda abierto tapando la
  // tabla es peor que no tenerlo.
  useEffect(() => {
    if (!abierto) return;
    function fuera(e: MouseEvent) {
      if (caja.current && !caja.current.contains(e.target as Node)) setAbierto(false);
    }
    function tecla(e: KeyboardEvent) {
      if (e.key === "Escape") setAbierto(false);
    }
    document.addEventListener("mousedown", fuera);
    document.addEventListener("keydown", tecla);
    return () => {
      document.removeEventListener("mousedown", fuera);
      document.removeEventListener("keydown", tecla);
    };
  }, [abierto]);

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
    setAbierto(false);
  }

  function moverMes(n: number) {
    setMes(new Date(mes.getFullYear(), mes.getMonth() + n, 1, 12));
  }

  const hoy = iso(new Date());

  return (
    <div className="rango" ref={caja}>
      <button
        type="button"
        className="pv-boton rango-boton"
        aria-expanded={abierto}
        onClick={() => {
          setMes(deISO(desde));
          setAncla(null);
          setAbierto((v) => !v);
        }}
      >
        {/* Iconos dibujados, no caracteres sueltos: un ▦ o un ▾ dependen de la
            fuente que tenga la máquina y se ven distintos en cada una. */}
        <svg className="rango-icono" viewBox="0 0 24 24" fill="none" aria-hidden>
          <rect x="3.5" y="5" width="17" height="15" rx="2" stroke="currentColor" strokeWidth="1.6" />
          <path d="M3.5 9.5h17M8 3.5v3M16 3.5v3" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
        </svg>
        <b>{comoSeLee(desde, hasta)}</b>
        <svg
          className="rango-flecha"
          viewBox="0 0 24 24"
          fill="none"
          data-abierto={abierto || undefined}
          aria-hidden
        >
          <path d="M6 9.5l6 6 6-6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {abierto && (
        <div className="calendario" onMouseLeave={() => setEncima(null)}>
          <div className="calendario-cabecera">
            <button className="pv-boton calendario-mes" onClick={() => moverMes(-1)} aria-label="Mes anterior">
              <svg viewBox="0 0 24 24" fill="none" aria-hidden>
                <path d="M14.5 6l-6 6 6 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </button>
            <b>
              {MESES[mes.getMonth()]} {mes.getFullYear()}
            </b>
            <button className="pv-boton calendario-mes" onClick={() => moverMes(1)} aria-label="Mes siguiente">
              <svg viewBox="0 0 24 24" fill="none" aria-hidden>
                <path d="M9.5 6l6 6-6 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
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
            {ancla ? "Marca el otro extremo" : "Toca el primer día y luego el último"}
          </p>
        </div>
      )}
    </div>
  );
}
