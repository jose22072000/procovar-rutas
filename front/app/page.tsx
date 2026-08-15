"use client";

/**
 * El calendario de cumplimiento: la pantalla de entrada.
 *
 * Una cuadrícula de vendedores × días workdays donde cada celda lleva el color
 * de su estado. De un vistazo se ve quién no está trabajando; se toca la celda y
 * se cae en el mapa de ese día. Es la respuesta a "cómo me entero": te enteras
 * porque el sistema lo pinta, no porque alguien abra los ficheros uno a uno.
 */

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  ESTADOS,
  fechaCorta,
  nombreDia,
  pedir,
  semanaLaboral,
  type EstadoDia,
  type RespuestaCalendario,
} from "@/lib/api";

function hoyISO(): string {
  return new Date().toISOString().slice(0, 10);
}

export default function Calendario() {
  const router = useRouter();
  const [ancla, setAncla] = useState(hoyISO());
  const [datos, setDatos] = useState<RespuestaCalendario | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [cargando, setCargando] = useState(true);

  const semana = useMemo(() => semanaLaboral(ancla), [ancla]);

  useEffect(() => {
    setCargando(true);
    setError(null);
    pedir<RespuestaCalendario>(
      `/api/calendar?from=${semana[0]}&to=${semana[4]}`,
    )
      .then(setDatos)
      .catch((e) => setError(e.message))
      .finally(() => setCargando(false));
  }, [semana]);

  // La cuadrícula se arma en el cliente: la API devuelve filas sueltas, y aquí
  // se cruzan seller × día para que falten celdas en vez de filas.
  const porVendedor = useMemo(() => {
    const mapa = new Map<
      string,
      { nombre: string; days: Map<string, RespuestaCalendario["days"][0]> }
    >();
    for (const d of datos?.days ?? []) {
      if (!mapa.has(d.sellerId)) {
        mapa.set(d.sellerId, { nombre: d.seller, days: new Map() });
      }
      mapa.get(d.sellerId)!.days.set(d.date.slice(0, 10), d);
    }
    return mapa;
  }, [datos]);

  function mover(days: number) {
    const d = new Date(`${ancla}T12:00:00Z`);
    setAncla(new Date(d.getTime() + days * 86400000).toISOString().slice(0, 10));
  }

  return (
    <>
      <h1>Calendario de cumplimiento</h1>
      <p className="sub">
        Lunes a viernes. Cada celda es un día: tócala para ver el track en el
        mapa.
      </p>

      <div className="controles">
        <button onClick={() => mover(-7)}>← Semana anterior</button>
        <input
          type="date"
          value={ancla}
          onChange={(e) => setAncla(e.target.value)}
        />
        <button onClick={() => mover(7)}>Semana siguiente →</button>
        <button onClick={() => setAncla(hoyISO())}>Esta semana</button>
      </div>

      {error && <p className="aviso">{error}</p>}
      {cargando && <p className="cargando">Cargando…</p>}

      {!cargando && porVendedor.size === 0 && !error && (
        <div className="tarjeta">
          <p>
            No hay datos para esta semana. Si es la primera vez, hay que dar de
            alta las carpetas de Drive en <b>Administración</b> y lanzar un
            barrido.
          </p>
        </div>
      )}

      {porVendedor.size > 0 && (
        <div className="tarjeta">
          <table className="rejilla">
            <thead>
              <tr>
                <th className="seller">Vendedor</th>
                {semana.map((f) => (
                  <th key={f}>
                    {nombreDia(f)} {fechaCorta(f)}
                  </th>
                ))}
                <th>Incidencias</th>
              </tr>
            </thead>
            <tbody>
              {[...porVendedor.entries()].map(([id, v]) => {
                const summary = datos?.summary.find(
                  (r) => r.sellerId === id,
                );
                const incidencias =
                  (summary?.daysNoFile ?? 0) +
                  (summary?.daysNoDate ?? 0) +
                  (summary?.daysNoMovement ?? 0);
                return (
                  <tr key={id}>
                    <td className="seller">{v.nombre}</td>
                    {semana.map((f) => {
                      const d = v.days.get(f);
                      // Sin fila para ese día se pinta como falta: es lo que es,
                      // y así una ausencia nunca queda invisible.
                      const estado: EstadoDia = d?.status ?? "SIN_FICHERO";
                      const est = ESTADOS[estado];
                      return (
                        <td key={f}>
                          <button
                            className="celda"
                            style={{ background: est.color, color: est.texto }}
                            title={`${v.nombre} · ${f} · ${est.etiqueta}`}
                            onClick={() =>
                              router.push(`/dia?seller=${id}&fecha=${f}`)
                            }
                          >
                            {estado === "OK" || estado === "MOVIMIENTO_ESCASO" ? (
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
                              est.etiqueta
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
              ] as EstadoDia[]
            ).map((e) => (
              <span key={e}>
                <i style={{ background: ESTADOS[e].color }} />
                {ESTADOS[e].etiqueta}
              </span>
            ))}
          </div>
        </div>
      )}
    </>
  );
}
