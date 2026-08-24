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
import SinPermiso from "@/components/SinPermiso";
import { useSesion } from "@/components/Sesion";
import {
  FLAG_LABEL,
  STATUS_LABEL,
  ask,
  type DayResponse,
} from "@/lib/api";
import type { Capas } from "@/components/RouteMap";

// Leaflet touches `window`: kept out of server rendering.
const MapaRuta = dynamic(() => import("@/components/RouteMap"), {
  ssr: false,
  loading: () => <div className="mapa-caja"><div className="mapa" /></div>,
});

function Visor() {
  const { cargando, vetado, puede } = useSesion();
  const params = useSearchParams();
  const seller = params.get("seller") ?? "";
  const fecha = params.get("fecha") ?? "";

  const [datos, setDatos] = useState<DayResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [completa, setCompleta] = useState(false);
  const [instante, setInstante] = useState(-1);
  // La parada o el cliente que se está mirando: se toca en la lista y el mapa lo
  // centra.
  const [paradaVista, setParadaVista] = useState<string | null>(null);
  const [clienteVisto, setClienteVisto] = useState<string | null>(null);

  // Las capas del mapa. Se entra con las tres encendidas, que es la vista completa;
  // apagando el recorrido quedan los clientes solos —la ruta que TENÍA que hacer— y
  // volviéndolo a encender se ve encima la que hizo. Esa comparación es justo para
  // lo que se abre esta pantalla.
  const [capas, setCapas] = useState<Capas>({ ruta: true, paradas: true, clientes: true });

  useEffect(() => {
    if (!seller || !fecha) return;
    setError(null);
    setDatos(null);
    setInstante(-1);
    setParadaVista(null);
    setClienteVisto(null);
    ask<DayResponse>(
      `/api/day?seller=${seller}&date=${fecha}${completa ? "&workday=full" : ""}`,
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

  if (cargando) return <p className="cargando">Cargando…</p>;
  if (vetado) return <SinPermiso que="Rutas" detalle={vetado.replace("sin permiso: ", "")} />;
  if (!puede("rutas.visor")) return <SinPermiso que="el recorrido" detalle="rutas.visor" />;

  if (!seller || !fecha) {
    return <p className="aviso">Falta el vendedor o la fecha.</p>;
  }


  return (
    <>
      {/* Cabecera y controles en una sola franja: lo que hay que ver aquí es el
          mapa, y cada línea de texto de arriba es un trozo de mapa menos. */}
      <div className="visor-cabecera no-imprimir">
        <div>
          <h1>{datos?.day.seller ?? "Recorrido"}</h1>
          <p className="sub">
            {fecha}
            {datos?.day.branch ? ` · ${datos.day.branch}` : ""}
          </p>
        </div>

        <div className="visor-acciones">
          <Link href={otroDia(-1)} className="pv-boton">← Día anterior</Link>
          <Link href={otroDia(1)} className="pv-boton">Día siguiente →</Link>
          <label className="pv-boton">
            <input
              type="checkbox"
              checked={completa}
              onChange={(e) => setCompleta(e.target.checked)}
            />{" "}
            Día completo
          </label>
          {/* Las capas: qué quiero ver encima del mapa. El de clientes solo aparece
              si hay clientes que enseñar — una casilla que no enciende nada es peor
              que no tenerla. */}
          <label className="pv-boton">
            <input
              type="checkbox"
              checked={capas.ruta}
              onChange={(e) => setCapas({ ...capas, ruta: e.target.checked })}
            />{" "}
            Recorrido
          </label>
          <label className="pv-boton">
            <input
              type="checkbox"
              checked={capas.paradas}
              onChange={(e) => setCapas({ ...capas, paradas: e.target.checked })}
            />{" "}
            Paradas
          </label>
          {(datos?.visits?.length ?? 0) > 0 && (
            <label className="pv-boton">
              <input
                type="checkbox"
                checked={capas.clientes}
                onChange={(e) => setCapas({ ...capas, clientes: e.target.checked })}
              />{" "}
              Clientes del día
            </label>
          )}
          <Link
            href={`/reporte?seller=${seller}&from=${fecha}&to=${fecha}`}
            className="pv-boton pv-boton-primario"
          >
            Reporte
          </Link>
        </div>
      </div>

      {error && <p className="aviso">{error}</p>}
      {!datos && !error && <p className="cargando">Cargando el recorrido…</p>}

      {datos && (
        <div className="visor">
          {/* El mapa primero y grande: es lo que se viene a ver. Los datos van al
              lado, apretados, no encima ni ocupando dos tercios como antes. */}
          <div className="visor-mapa">
            <MapaRuta
              points={datos.points}
              stops={datos.stops}
              visits={datos.visits ?? []}
              capas={capas}
              to={instante}
              focusStopId={paradaVista}
              focusClientId={clienteVisto}
            />

            {datos.points.length > 0 && (
              <div className="linea-tiempo no-imprimir">
                <input
                  type="range"
                  min={0}
                  max={datos.points.length - 1}
                  value={instante < 0 ? datos.points.length - 1 : instante}
                  onChange={(e) => setInstante(Number(e.target.value))}
                />
                <span className="pv-codigo">
                  {hora(
                    datos.points[instante < 0 ? datos.points.length - 1 : instante].ts,
                  )}
                </span>
                <button className="pv-boton" onClick={() => setInstante(-1)}>
                  Todo el día
                </button>
              </div>
            )}
          </div>

          <aside className="visor-datos">
            <div className="celda" data-status={datos.day.status}>
              {STATUS_LABEL[datos.day.status]}
            </div>

            {/* Fichas pequeñas en rejilla: los mismos ocho datos que antes ocupaban
                ocho renglones, en tres líneas. */}
            <div className="cifras">
              <Cifra rotulo="Kilómetros" valor={`${datos.day.netKm.toFixed(1)} km`} />
              <Cifra rotulo="Con señal" valor={`${datos.day.coverage.toFixed(0)} %`} />
              <Cifra rotulo="Empezó" valor={hora(datos.day.firstFix)} />
              <Cifra rotulo="Terminó" valor={hora(datos.day.lastFix)} />
              <Cifra rotulo="En marcha" valor={`${datos.day.minMovement} min`} />
              <Cifra rotulo="Parado" valor={`${datos.day.minStopped} min`} />
              <Cifra rotulo="Paradas" valor={String(datos.stops.length)} />
              {datos.visits?.length > 0 && (
                <Cifra
                  rotulo="Clientes"
                  valor={`${datos.visits.filter((v) => v.visited).length}/${datos.visits.length}`}
                />
              )}
              {datos.day.spreadM !== null && (
                <Cifra rotulo="Se alejó" valor={`${datos.day.spreadM} m`} />
              )}
            </div>

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
              <p className="sub">
                Los puntos no se alejaron del mismo lugar en toda la jornada.
                {datos.day.placeLabel ? ` Estuvo en ${datos.day.placeLabel}.` : ""}
              </p>
            )}

            {/* Los clientes del día ANTES que las paradas: es la pregunta con la
                que se abre esta pantalla —¿pasó por los suyos?— y las paradas son
                el detalle de cómo. */}
            {datos.visits?.length > 0 && (
              <div className="paradas">
                <div className="pv-rotulo">
                  Clientes del día · {datos.visits.filter((v) => v.visited).length} de{" "}
                  {datos.visits.length} visitados
                </div>
                <ul>
                  {datos.visits.map((v) => (
                    <li key={v.id}>
                      <button
                        className="parada-fila"
                        data-vista={clienteVisto === v.clientId}
                        data-visitado={v.visited}
                        onClick={() => {
                          setClienteVisto(v.clientId);
                          setCapas((c) => ({ ...c, clientes: true }));
                        }}
                      >
                        <span className="pv-codigo">
                          {v.visited ? hora(v.time) : "—"}
                        </span>
                        <span className="parada-min">
                          {v.visited
                            ? `${v.minutes ?? 0} min`
                            : v.distanceM != null
                              ? `a ${Math.round(v.distanceM)} m`
                              : "sin datos"}
                        </span>
                        <span className="parada-donde">{v.clientName}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {datos.stops.length > 0 && (
              <div className="paradas">
                <div className="pv-rotulo">Paradas</div>
                <ul>
                  {datos.stops.map((p) => (
                    <li key={p.id}>
                      {/* Tocar la parada la centra en el mapa: la lista y el dibujo
                          son la misma cosa mirada de dos maneras. */}
                      <button
                        className="parada-fila"
                        data-vista={paradaVista === p.id}
                        onClick={() => {
                          setParadaVista(p.id);
                          setCapas((c) => ({ ...c, paradas: true }));
                        }}
                      >
                        <span className="pv-codigo">
                          {hora(p.start)}–{hora(p.end)}
                        </span>
                        <span className="parada-min">{p.durationMin} min</span>
                        <span className="parada-donde">{p.clientName ?? "—"}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </aside>
        </div>
      )}
    </>
  );
}

// Una cifra suelta: rótulo pequeño arriba, número grande abajo. Se lee de un
// vistazo, que es de lo que se trata en una pantalla que se mira todo el día.
function Cifra({ rotulo, valor }: { rotulo: string; valor: string }) {
  return (
    <div className="cifra">
      <span className="pv-rotulo">{rotulo}</span>
      <b>{valor}</b>
    </div>
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
