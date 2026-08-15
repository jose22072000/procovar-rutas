"use client";

/**
 * El visor: un vendedor, un día, su recorrido en el mapa.
 *
 * Es lo que sustituye a descargar el .gpx y abrirlo en una aplicación externa.
 * Por defecto se recorta a la jornada (9:00–16:00), con interruptor para ver el
 * día completo — porque el control es de la jornada, pero a veces hace falta ver
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
  const vendedor = params.get("vendedor") ?? "";
  const fecha = params.get("fecha") ?? "";

  const [datos, setDatos] = useState<RespuestaDia | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [completa, setCompleta] = useState(false);
  const [instante, setInstante] = useState(-1);

  useEffect(() => {
    if (!vendedor || !fecha) return;
    setError(null);
    setDatos(null);
    setInstante(-1);
    pedir<RespuestaDia>(
      `/api/dia?vendedor=${vendedor}&fecha=${fecha}${completa ? "&jornada=completa" : ""}`,
    )
      .then(setDatos)
      .catch((e) => setError(e.message));
  }, [vendedor, fecha, completa]);

  function otroDia(dias: number): string {
    const d = new Date(`${fecha}T12:00:00Z`);
    const nueva = new Date(d.getTime() + dias * 86400000)
      .toISOString()
      .slice(0, 10);
    return `/dia?vendedor=${vendedor}&fecha=${nueva}`;
  }

  if (!vendedor || !fecha) {
    return <p className="aviso">Falta el vendedor o la fecha.</p>;
  }

  const est = datos ? ESTADOS[datos.dia.Estado] : null;

  return (
    <>
      <h1>{datos?.dia.Trabajador ?? "Recorrido"}</h1>
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
        <Link href={`/reporte?vendedor=${vendedor}&fecha=${fecha}`}>
          <button className="primario">Reporte de la semana</button>
        </Link>
      </div>

      {error && <p className="aviso">{error}</p>}
      {!datos && !error && <p className="cargando">Cargando el recorrido…</p>}

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
                <span>{datos.dia.KmNetos.toFixed(2)} km</span>
              </div>
              <div className="dato">
                <span>Primer fix</span>
                <span>{hora(datos.dia.PrimerFix)}</span>
              </div>
              <div className="dato">
                <span>Último fix</span>
                <span>{hora(datos.dia.UltimoFix)}</span>
              </div>
              <div className="dato">
                <span>Cobertura</span>
                <span>{datos.dia.Cobertura.toFixed(0)} %</span>
              </div>
              <div className="dato">
                <span>En movimiento</span>
                <span>{datos.dia.MinMovimiento} min</span>
              </div>
              <div className="dato">
                <span>Parado</span>
                <span>{datos.dia.MinParado} min</span>
              </div>
              <div className="dato">
                <span>Paradas</span>
                <span>{datos.paradas.length}</span>
              </div>
              {datos.dia.RadioDispersion !== null && (
                <div className="dato">
                  <span>Radio del día</span>
                  <span>{datos.dia.RadioDispersion} m</span>
                </div>
              )}

              {datos.dia.Banderas.length > 0 && (
                <div className="banderas">
                  {datos.dia.Banderas.map((b) => (
                    <span key={b} className="bandera">
                      {BANDERAS[b] ?? b}
                    </span>
                  ))}
                </div>
              )}

              {datos.dia.Estado === "SIN_MOVIMIENTO" && (
                <p className="sub" style={{ marginTop: "0.75rem" }}>
                  Los puntos no se alejaron del mismo lugar en toda la jornada.
                  {datos.dia.LugarTexto ? ` Estuvo en ${datos.dia.LugarTexto}.` : ""}
                </p>
              )}
            </div>

            {datos.paradas.length > 0 && (
              <div className="tarjeta">
                <b>Paradas</b>
                <table className="movimientos">
                  <tbody>
                    {datos.paradas.map((p) => (
                      <tr key={p.ID} className="parada">
                        <td>
                          {hora(p.Inicio)}–{hora(p.Fin)}
                        </td>
                        <td>{p.DuracionMin} min</td>
                        <td>{p.ClienteNombre ?? "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          <div>
            <MapaRuta
              puntos={datos.puntos}
              paradas={datos.paradas}
              hasta={instante}
            />
            {datos.puntos.length > 0 && (
              <div className="controles" style={{ marginTop: "0.75rem" }}>
                <input
                  type="range"
                  min={0}
                  max={datos.puntos.length - 1}
                  value={instante < 0 ? datos.puntos.length - 1 : instante}
                  onChange={(e) => setInstante(Number(e.target.value))}
                  style={{ flex: 1, padding: 0 }}
                />
                <span style={{ color: "var(--tenue)", minWidth: 120 }}>
                  {hora(
                    datos.puntos[
                      instante < 0 ? datos.puntos.length - 1 : instante
                    ].Ts,
                  )}{" "}
                  ({datos.puntos.length} puntos)
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
