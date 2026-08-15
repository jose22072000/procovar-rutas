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
  type DayStatus,
  type CalendarResponse,
} from "@/lib/api";

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

export default function Calendar() {
  const router = useRouter();
  const [anchor, setAnchor] = useState(todayISO());
  const [data, setData] = useState<CalendarResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const week = useMemo(() => workWeek(anchor), [anchor]);

  useEffect(() => {
    setLoading(true);
    setError(null);
    ask<CalendarResponse>(
      `/api/calendar?from=${week[0]}&to=${week[4]}`,
    )
      .then(setData)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [week]);

  // The grid is assembled on the client: the API returns loose rows, and seller ×
  // day are crossed here so that cells go missing rather than whole rows.
  const bySeller = useMemo(() => {
    const map = new Map<
      string,
      { name: string; days: Map<string, CalendarResponse["days"][0]> }
    >();
    for (const d of data?.days ?? []) {
      if (!map.has(d.sellerId)) {
        map.set(d.sellerId, { name: d.seller, days: new Map() });
      }
      map.get(d.sellerId)!.days.set(d.date.slice(0, 10), d);
    }
    return map;
  }, [data]);

  function mover(days: number) {
    const d = new Date(`${anchor}T12:00:00Z`);
    setAnchor(new Date(d.getTime() + days * 86400000).toISOString().slice(0, 10));
  }

  return (
    <>
      <h1>Calendario de cumplimiento</h1>
      <p className="sub">
        Lunes a viernes. Cada celda es un día: tócala para ver el track en el
        map.
      </p>

      <div className="controles">
        <button className="pv-boton" onClick={() => mover(-7)}>← Semana anterior</button>
        <input
          type="date"
          value={anchor}
          onChange={(e) => setAnchor(e.target.value)}
        />
        <button className="pv-boton" onClick={() => mover(7)}>Semana siguiente →</button>
        <button className="pv-boton" onClick={() => setAnchor(todayISO())}>Esta semana</button>
      </div>

      {error && <p className="aviso">{error}</p>}
      {loading && <p className="loading">Cargando…</p>}

      {!loading && bySeller.size === 0 && !error && (
        <div className="tarjeta">
          <p>
            No hay data para esta semana. Si es la primera vez, hay que dar de
            alta las carpetas de Drive en <b>Administración</b> y lanzar un
            barrido.
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
                    <td className="seller">{v.name}</td>
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
