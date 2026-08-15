"use client";

/**
 * The viewer: one seller, one day, their track on the map.
 *
 * It is what replaces downloading the .gpx and opening it in some other program.
 * By default it is trimmed to working hours (9:00–16:00), with a switch to see the
 * full day — because the tracking is about the workday, but sometimes you need to
 * see what happened outside it.
 */

import { Suspense, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import {
  FLAG_LABEL,
  STATUS_LABEL,
  ask,
  type DayResponse,
} from "@/lib/api";

// Leaflet touches `window`: kept out of server rendering.
const MapaRuta = dynamic(() => import("@/components/RouteMap"), {
  ssr: false,
  loading: () => <div className="mapa" />,
});

function Visor() {
  const params = useSearchParams();
  const seller = params.get("seller") ?? "";
  const fecha = params.get("fecha") ?? "";

  const [datos, setDatos] = useState<DayResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [completa, setCompleta] = useState(false);
  const [instante, setInstante] = useState(-1);

  useEffect(() => {
    if (!seller || !fecha) return;
    setError(null);
    setDatos(null);
    setInstante(-1);
    ask<DayResponse>(
      `/api/day?seller=${seller}&fecha=${fecha}${completa ? "&workday=completa" : ""}`,
    )
      .then(setDatos)
      .catch((e) => setError(e.message));
  }, [seller, fecha, completa]);

  function otroDia(days: number): string {
    const d = new Date(`${fecha}T12:00:00Z`);
    const nueva = new Date(d.getTime() + days * 86400000)
      .toISOString()
      .slice(0, 10);
    return `/dia?seller=${seller}&fecha=${nueva}`;
  }

  if (!seller || !fecha) {
    return <p className="aviso">Falta el vendedor o la fecha.</p>;
  }


  return (
    <>
      <h1>{datos?.day.seller ?? "Recorrido"}</h1>
      <p className="sub">{fecha}</p>

      <div className="controles">
        <Link href={otroDia(-1)}>
          <button>← Día anterior</button>
        </Link>
        <Link href={otroDia(1)}>
          <button>Día siguiente →</button>
        </Link>
        <label>
          <input
            type="checkbox"
            checked={completa}
            onChange={(e) => setCompleta(e.target.checked)}
          />{" "}
          Ver el día completo (fuera de 9:00–16:00)
        </label>
        <Link href={`/reporte?seller=${seller}&from=${fecha}&to=${fecha}`}>
          <button className="pv-boton pv-boton-primario">Reporte de la semana</button>
        </Link>
      </div>

      {error && <p className="aviso">{error}</p>}
      {!datos && !error && <p className="cargando">Cargando el track…</p>}

      {datos && (
        <div className="visor">
          <div>
            <div className="tarjeta">
              {/* The status pill takes its colour from the stylesheet through
                  data-status, like the calendar cells. */}
              <div className="celda" data-status={datos.day.status} style={{ marginBottom: "0.75rem" }}>
                {STATUS_LABEL[datos.day.status]}
              </div>

              <div className="dato">
                <span>Kilómetros</span>
                <span>{datos.day.netKm.toFixed(2)} km</span>
              </div>
              <div className="dato">
                <span>Primer fix</span>
                <span>{hora(datos.day.firstFix)}</span>
              </div>
              <div className="dato">
                <span>Último fix</span>
                <span>{hora(datos.day.lastFix)}</span>
              </div>
              <div className="dato">
                <span>Cobertura</span>
                <span>{datos.day.coverage.toFixed(0)} %</span>
              </div>
              <div className="dato">
                <span>En movimiento</span>
                <span>{datos.day.minMovement} min</span>
              </div>
              <div className="dato">
                <span>Parado</span>
                <span>{datos.day.minStopped} min</span>
              </div>
              <div className="dato">
                <span>Paradas</span>
                <span>{datos.stops.length}</span>
              </div>
              {datos.day.spreadM !== null && (
                <div className="dato">
                  <span>Radio del día</span>
                  <span>{datos.day.spreadM} m</span>
                </div>
              )}

              {datos.day.flags.length > 0 && (
                <div className="flags">
                  {datos.day.flags.map((b) => (
                    <span key={b} className="bandera">
                      {FLAG_LABEL[b] ?? b}
                    </span>
                  ))}
                </div>
              )}

              {datos.day.status === "SIN_MOVIMIENTO" && (
                <p className="sub" style={{ marginTop: "0.75rem" }}>
                  Los points no se alejaron del mismo lugar en toda la workday.
                  {datos.day.placeLabel ? ` Estuvo en ${datos.day.placeLabel}.` : ""}
                </p>
              )}
            </div>

            {datos.stops.length > 0 && (
              <div className="tarjeta">
                <b>Paradas</b>
                <table className="movements">
                  <tbody>
                    {datos.stops.map((p) => (
                      <tr key={p.id} className="parada">
                        <td>
                          {hora(p.start)}–{hora(p.end)}
                        </td>
                        <td>{p.durationMin} min</td>
                        <td>{p.clientName ?? "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div>
            <MapaRuta
              points={datos.points}
              stops={datos.stops}
              to={instante}
            />
            {datos.points.length > 0 && (
              <div className="controles" style={{ marginTop: "0.75rem" }}>
                <input
                  type="range"
                  min={0}
                  max={datos.points.length - 1}
                  value={instante < 0 ? datos.points.length - 1 : instante}
                  onChange={(e) => setInstante(Number(e.target.value))}
                  style={{ flex: 1, padding: 0 }}
                />
                <span style={{ color: "var(--tenue)", minWidth: 120 }}>
                  {hora(
                    datos.points[
                      instante < 0 ? datos.points.length - 1 : instante
                    ].ts,
                  )}{" "}
                  ({datos.points.length} points)
                </span>
                <button className="pv-boton" onClick={() => setInstante(-1)}>Todo el día</button>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}

function hora(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString("es", {
    hour: "2-digit",
    minute: "2-digit",
  });
}

export default function Pagina() {
  return (
    <Suspense fallback={<p className="cargando">Cargando…</p>}>
      <Visor />
    </Suspense>
  );
}
