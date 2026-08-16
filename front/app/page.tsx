"use client";

/**
 * The compliance calendar: the landing screen.
 *
 * A grid of sellers × working days where each cell carries the colour of its
 * status. At a glance you see who is not working; touch a cell and you land on
 * that day's map. It is the answer to "how do I find out": you find out because
 * the system paints it, not because somebody opens the files one by one.
 *
 * The colour lives in the stylesheet, keyed off data-status, and never in inline
 * styles: the palette is Procovar's, shared with Accesos and PEDIDO, and a colour
 * written here would drift from the others the first time one of them changes.
 */

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  STATUS_LABEL,
  shortDate,
  dayName,
  ask,
  workWeek,
  diasEntre,
  type DayStatus,
  type CalendarResponse,
} from "@/lib/api";

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

export default function Calendar() {
  const router = useRouter();
  // Desde/hasta, no un día suelto: el control lo pediste así y además es lo mismo
  // que ya acepta el API. La semana laboral de hoy es el punto de partida, que es
  // lo que se quiere ver al entrar.
  // Un solo calendario: se elige un día y al lado cuánto se quiere ver desde él.
  // Dos campos de fecha eran dos cosas que cuadrar a mano para lo que casi siempre
  // es "la semana de este día".
  const [dia, setDia] = useState(todayISO());
  const [tramo, setTramo] = useState<"semana" | "dia" | "quincena" | "mes">("semana");

  const [desde, hasta] = useMemo(() => rango(dia, tramo), [dia, tramo]);
  const [data, setData] = useState<CalendarResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Los días del rango, uno por columna. Se calculan aquí y no en el API porque es
  // la cuadrícula la que necesita saber qué columnas pintar, incluso las vacías.
  const week = useMemo(() => diasEntre(desde, hasta), [desde, hasta]);

  useEffect(() => {
    setLoading(true);
    setError(null);
    ask<CalendarResponse>(
      `/api/calendar?from=${desde}&to=${hasta}`,
    )
      .then(setData)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [desde, hasta]);

  // The grid is assembled on the client: the API returns loose rows, and seller ×
  // day are crossed here so that cells go missing rather than whole rows.
  const bySeller = useMemo(() => {
    const map = new Map<
      string,
      { name: string; branch: string; days: Map<string, CalendarResponse["days"][0]> }
    >();
    for (const d of data?.days ?? []) {
      if (!map.has(d.sellerId)) {
        map.set(d.sellerId, { name: d.seller, branch: d.branch, days: new Map() });
      }
      map.get(d.sellerId)!.days.set(d.date.slice(0, 10), d);
    }
    return map;
  }, [data]);

  function mover(days: number) {
    setDia(
      new Date(new Date(`${dia}T12:00:00Z`).getTime() + days * 86400000)
        .toISOString()
        .slice(0, 10),
    );
  }

  // De un día y un tramo, el rango. La semana es de lunes a viernes, que es la
  // jornada de la empresa; los demás tramos van desde el día elegido.
  function rango(d: string, t: string): [string, string] {
    if (t === "semana") {
      const s = workWeek(d);
      return [s[0], s[4]];
    }
    const dias = t === "dia" ? 0 : t === "quincena" ? 14 : 29;
    const fin = new Date(new Date(`${d}T12:00:00Z`).getTime() + dias * 86400000)
      .toISOString()
      .slice(0, 10);
    return [d, fin];
  }

  return (
    <>
      <h1>Calendario de cumplimiento</h1>
      <p className="sub">
        Cada celda es un día: tócala para ver el recorrido en el mapa.
      </p>

      <div className="controles">
        <button className="pv-boton" onClick={() => mover(-7)}>← Semana anterior</button>
        <input
          className="pv-campo"
          type="date"
          value={dia}
          onChange={(e) => setDia(e.target.value)}
        />
        <select
          className="pv-campo"
          value={tramo}
          onChange={(e) => setTramo(e.target.value as typeof tramo)}
        >
          <option value="semana">Su semana (L–V)</option>
          <option value="dia">Solo ese día</option>
          <option value="quincena">15 días desde ahí</option>
          <option value="mes">30 días desde ahí</option>
        </select>
        <button className="pv-boton" onClick={() => mover(7)}>Semana siguiente →</button>
        <button
          className="pv-boton"
          onClick={() => {
            setDia(todayISO());
            setTramo("semana");
          }}
        >
          Esta semana
        </button>
      </div>

      {error && <p className="aviso">{error}</p>}
      {loading && <p className="loading">Cargando…</p>}

      {!loading && bySeller.size === 0 && !error && (
        <div className="tarjeta">
          <p>
            No hay datos en ese rango. Si es la primera vez, las carpetas de Drive
            se dan de alta solas en cuanto la ingesta de n8n empuja el primer
            fichero de cada una.
          </p>
        </div>
      )}

      {bySeller.size > 0 && (
        <div className="tarjeta">
          <table className="rejilla">
            <thead>
              <tr>
                <th className="seller">Vendedor</th>
                {week.map((f) => (
                  <th key={f}>
                    {dayName(f)} {shortDate(f)}
                  </th>
                ))}
                <th>Incidencias</th>
              </tr>
            </thead>
            <tbody>
              {[...bySeller.entries()].map(([id, v]) => {
                const summary = data?.summary.find(
                  (r) => r.sellerId === id,
                );
                const incidencias =
                  (summary?.daysNoFile ?? 0) +
                  (summary?.daysNoDate ?? 0) +
                  (summary?.daysNoMovement ?? 0);
                return (
                  <tr key={id}>
                    <td className="seller">
                      {v.name}
                      {/* La sucursal, debajo del nombre: es lo que dice si la
                          ingesta colocó a cada quien donde tocaba, y lo que el
                          gerente necesita reconocer de un vistazo. */}
                      <span className="seller-sucursal">{v.branch || "sin sucursal"}</span>
                    </td>
                    {week.map((f) => {
                      const d = v.days.get(f);
                      // No row for that day is painted as a miss: that is what it
                      // is, and this way an absence is never invisible.
                      const status: DayStatus = d?.status ?? "SIN_FICHERO";
                      const label = STATUS_LABEL[status];
                      return (
                        <td key={f}>
                          <button
                            className="celda"
                            data-status={status}
                            title={`${v.name} · ${f} · ${label}`}
                            onClick={() =>
                              router.push(`/dia?seller=${id}&fecha=${f}`)
                            }
                          >
                            {status === "OK" || status === "MOVIMIENTO_ESCASO" ? (
                              <>
                                <span className="km">
                                  {d?.netKm.toFixed(1)} km
                                </span>
                                {d?.firstFix
                                  ? new Date(d.firstFix).toLocaleTimeString(
                                      "es",
                                      { hour: "2-digit", minute: "2-digit" },
                                    )
                                  : ""}
                              </>
                            ) : (
                              label
                            )}
                          </button>
                        </td>
                      );
                    })}
                    <td>
                      <div className="contadores">
                        {incidencias > 0 ? (
                          <span
                            className="pastilla"
                            style={{
                              background: "var(--falta)",
                              color: "#fff",
                            }}
                          >
                            {incidencias}
                          </span>
                        ) : (
                          <span
                            className="pastilla"
                            style={{ background: "var(--ok)" }}
                          >
                            0
                          </span>
                        )}
                        <span style={{ color: "var(--tenue)" }}>
                          {(summary?.totalKm ?? 0).toFixed(0)} km
                        </span>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          <div className="leyenda">
            {(
              [
                "OK",
                "SIN_FICHERO",
                "SIN_FECHA",
                "SIN_MOVIMIENTO",
                "MOVIMIENTO_ESCASO",
              ] as DayStatus[]
            ).map((e) => (
              // The legend reuses the very same cell so the colours cannot drift
              // from the grid's.
              <span key={e} className="celda pastilla" data-status={e}>
                {STATUS_LABEL[e]}
              </span>
            ))}
          </div>
        </div>
      )}
    </>
  );
}
