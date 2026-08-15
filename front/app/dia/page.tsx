"use client";

/**
 * El visor: un seller, un día, su track en el mapa.
 *
 * Es lo que sustituye a descargar el .gpx y abrirlo en una aplicación externa.
 * Por defecto se recorta a la workday (9:00–16:00), con interruptor para ver el
 * día completo — porque el control es de la workday, pero a veces hace falta ver
 * qué pasó fuera de ella.
 */

import { Suspense, useEffect, useState } from "react";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import {
  BANDERAS,
  ESTADOS,
  pedir,
  type RespuestaDia,
} from "@/lib/api";

// Leaflet toca `window`: fuera del renderizado en servidor.
const MapaRuta = dynamic(() => import("@/components/MapaRuta"), {
  ssr: false,
  loading: () => <div className="mapa" />,
});

function Visor() {
  const params = useSearchParams();
  const seller = params.get("seller") ?? "";
  const fecha = params.get("fecha") ?? "";

  const [datos, setDatos] = useState<RespuestaDia | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [completa, setCompleta] = useState(false);
  const [instante, setInstante] = useState(-1);

  useEffect(() => {
    if (!seller || !fecha) return;
    setError(null);
    setDatos(null);
    setInstante(-1);
    pedir<RespuestaDia>(
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

  const est = datos ? ESTADOS[datos.day.status] : null;

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
          <button className="primario">Reporte de la semana</button>
        </Link>
      </div>

      {error && <p className="aviso">{error}</p>}
      {!datos && !error && <p className="cargando">Cargando el track…</p>}

      {datos && (
        <div className="visor">
          <div>
            <div className="tarjeta">
              <div
                className="pastilla"
                style={{
                  background: est!.color,
                  color: est!.texto,
                  display: "inline-block",
                  marginBottom: "0.75rem",
                }}
              >
                {est!.etiqueta}
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
                      {BANDERAS[b] ?? b}
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
                <button onClick={() => setInstante(-1)}>Todo el día</button>
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
