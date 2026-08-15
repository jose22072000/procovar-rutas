"use client";

/**
 * El calendario de cumplimiento: la pantalla de entrada.
 *
 * Una cuadrícula de vendedores × días laborables donde cada celda lleva el color
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
      `/api/calendario?desde=${semana[0]}&hasta=${semana[4]}`,
    )
      .then(setDatos)
      .catch((e) => setError(e.message))
      .finally(() => setCargando(false));
  }, [semana]);

  // La cuadrícula se arma en el cliente: la API devuelve filas sueltas, y aquí
  // se cruzan vendedor × día para que falten celdas en vez de filas.
  const porVendedor = useMemo(() => {
    const mapa = new Map<
      string,
      { nombre: string; dias: Map<string, RespuestaCalendario["dias"][0]> }
    >();
    for (const d of datos?.dias ?? []) {
      if (!mapa.has(d.TrabajadorID)) {
        mapa.set(d.TrabajadorID, { nombre: d.Trabajador, dias: new Map() });
      }
      mapa.get(d.TrabajadorID)!.dias.set(d.Fecha.slice(0, 10), d);
    }
    return mapa;
  }, [datos]);

  function mover(dias: number) {
    const d = new Date(`${ancla}T12:00:00Z`);
    setAncla(new Date(d.getTime() + dias * 86400000).toISOString().slice(0, 10));
  }

  return (
    <>
      <h1>Calendario de cumplimiento</h1>
      <p className="sub">
        Lunes a viernes. Cada celda es un día: tócala para ver el recorrido en el
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
                <th className="vendedor">Vendedor</th>
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
                const resumen = datos?.resumen.find(
                  (r) => r.TrabajadorID === id,
                );
                const incidencias =
                  (resumen?.SinFichero ?? 0) +
                  (resumen?.SinFecha ?? 0) +
                  (resumen?.SinMovimiento ?? 0);
                return (
                  <tr key={id}>
                    <td className="vendedor">{v.nombre}</td>
                    {semana.map((f) => {
                      const d = v.dias.get(f);
                      // Sin fila para ese día se pinta como falta: es lo que es,
                      // y así una ausencia nunca queda invisible.
                      const estado: EstadoDia = d?.Estado ?? "SIN_FICHERO";
                      const est = ESTADOS[estado];
                      return (
                        <td key={f}>
                          <button
                            className="celda"
                            style={{ background: est.color, color: est.texto }}
                            title={`${v.nombre} · ${f} · ${est.etiqueta}`}
                            onClick={() =>
                              router.push(`/dia?vendedor=${id}&fecha=${f}`)
                            }
                          >
                            {estado === "OK" || estado === "MOVIMIENTO_ESCASO" ? (
                              <>
                                <span className="km">
                                  {d?.KmNetos.toFixed(1)} km
                                </span>
                                {d?.PrimerFix
                                  ? new Date(d.PrimerFix).toLocaleTimeString(
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
                          {(resumen?.KmTotal ?? 0).toFixed(0)} km
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
