"use client";

/**
 * A seller's report: every movement detailed between 9:00 and 16:00.
 *
 * It is printed to PDF from the browser itself (Ctrl+P → Save as PDF). There is
 * deliberately no PDF generator on the server: the browser already knows how to
 * paginate, and a second layout in the backend would be one more thing to keep in
 * sync with this one.
 */

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ask } from "@/lib/api";

interface Movimiento {
  type: string;
  startTime: string;
  endTime: string;
  durationMin: number;
  distanceKm: number;
  avgSpeed: number;
  maxSpeed: number;
  place?: string;
}

interface DiaReporte {
  date: string;
  status: string;
  reason?: string;
  firstFix?: string;
  lastFix?: string;
  netKm: number;
  coverage: number;
  minStopped: number;
  minMovement: number;
  flags: string[];
  movements: Movimiento[];
  place?: string;
}

interface Documento {
  header: {
    seller: string;
    from: string;
    to: string;
    workday: string;
  };
  summary: {
    daysOk: number;
    daysNoFile: number;
    daysNoDate: number;
    daysNoMovement: number;
    totalKm: number;
    stops: number;
    avgCoverage: number;
  };
  days: DiaReporte[];
}

function Reporte() {
  const params = useSearchParams();
  const router = useRouter();
  const seller = params.get("seller") ?? "";
  // The range is free: ?from= and ?to=. Without them the API returns the current
  // week, which is how this report was asked for before it accepted loose dates.
  const from = params.get("from") ?? "";
  const to = params.get("to") ?? "";
  const [doc, setDoc] = useState<Documento | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Changing the range is navigation: the URL is the source of truth, which is
  // what makes the back button and a copied link work.
  function cambiarRango(nuevoDesde: string, nuevoHasta: string) {
    const p = new URLSearchParams({ seller });
    if (nuevoDesde && nuevoHasta) {
      p.set("from", nuevoDesde);
      p.set("to", nuevoHasta);
    }
    router.replace(`/reporte?${p.toString()}`);
  }

  useEffect(() => {
    if (!seller) return;
    const rango = from && to ? `&from=${from}&to=${to}` : "";
    ask<Documento>(`/api/report?seller=${seller}${rango}`)
      .then(setDoc)
      .catch((e) => setError(e.message));
  }, [seller, from, to]);

  if (error) return <p className="aviso">{error}</p>;
  if (!doc) return <p className="cargando">Armando el reporte…</p>;

  const incidencias =
    doc.summary.daysNoFile +
    doc.summary.daysNoDate +
    doc.summary.daysNoMovement;

  return (
    <>
      <div className="controles no-imprimir">
        {/* The range is edited right here and kept in the URL: that way a report
            for a few particular days can be sent over chat as it is. */}
        <label>
          Desde{" "}
          <input
            className="pv-campo"
            type="date"
            value={from || doc.header.from}
            onChange={(e) => cambiarRango(e.target.value, to || doc.header.to)}
          />
        </label>
        <label>
          Hasta{" "}
          <input
            className="pv-campo"
            type="date"
            value={to || doc.header.to}
            onChange={(e) => cambiarRango(from || doc.header.from, e.target.value)}
          />
        </label>
        <button className="pv-boton" onClick={() => cambiarRango("", "")}>Semana en curso</button>
        <button className="pv-boton pv-boton-primario" onClick={() => window.print()}>
          Imprimir / Guardar como PDF
        </button>
      </div>

      <h1>{doc.header.seller}</h1>
      <p className="sub">
        Del {doc.header.from} al {doc.header.to} · jornada{" "}
        {doc.header.workday}
      </p>

      <div className="tarjeta">
        <b>Resumen de la semana</b>
        <div className="dato">
          <span>Días trabajados</span>
          <span>{doc.summary.daysOk} de 5</span>
        </div>
        <div className="dato">
          <span>Incidencias</span>
          <span>
            {incidencias === 0 ? "ninguna" : incidencias}
            {incidencias > 0 && (
              <>
                {" "}
                ({doc.summary.daysNoFile} sin fichero,{" "}
                {doc.summary.daysNoDate} sin fecha,{" "}
                {doc.summary.daysNoMovement} sin moverse)
              </>
            )}
          </span>
        </div>
        <div className="dato">
          <span>Kilómetros</span>
          <span>{doc.summary.totalKm.toFixed(2)} km</span>
        </div>
        <div className="dato">
          <span>Paradas</span>
          <span>{doc.summary.stops}</span>
        </div>
        <div className="dato">
          <span>Cobertura media</span>
          <span>{doc.summary.avgCoverage.toFixed(0)} %</span>
        </div>
      </div>

      {doc.days.map((d) => (
        <div className="tarjeta" key={d.date}>
          <b>{d.date}</b>
          {d.reason && <p className="sub">{d.reason}</p>}

          {d.movements.length > 0 ? (
            <>
              <p className="sub">
                {d.firstFix}–{d.lastFix} · {d.netKm.toFixed(2)} km ·{" "}
                {d.coverage.toFixed(0)} % de cobertura · {d.minMovement} min
                en movimiento, {d.minStopped} min parado
              </p>
              <table className="movements">
                <thead>
                  <tr>
                    <th>Inicio</th>
                    <th>Fin</th>
                    <th>Duración</th>
                    <th>Tipo</th>
                    <th>Distancia</th>
                    <th>Vel. media</th>
                    <th>Vel. máx</th>
                    <th>Lugar</th>
                  </tr>
                </thead>
                <tbody>
                  {d.movements.map((m, i) => (
                    <tr key={i} className={m.type === "parada" ? "parada" : ""}>
                      <td>{m.startTime}</td>
                      <td>{m.endTime}</td>
                      <td>{m.durationMin} min</td>
                      <td>
                        {m.type === "parada" ? <b>Parada</b> : "Desplazamiento"}
                      </td>
                      <td>{m.type === "parada" ? "—" : `${m.distanceKm} km`}</td>
                      <td>{m.type === "parada" ? "—" : `${m.avgSpeed} km/h`}</td>
                      <td>{m.type === "parada" ? "—" : `${m.maxSpeed} km/h`}</td>
                      <td>{m.place ?? "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : (
            d.place && <p className="sub">Estuvo en {d.place}.</p>
          )}
        </div>
      ))}

      <p className="sub">
        El GPS indica dónde estuvo el teléfono, no necesariamente la persona. Un
        hueco de señal bajo techo se parece a un GPS apagado: estos datos sirven
        para revisar, no para sancionar por sí solos.
      </p>

      <style jsx global>{`
        @media print {
          .barra,
          .no-imprimir {
            display: none;
          }
          .tarjeta {
            break-inside: avoid;
            border-color: #ccc;
          }
        }
      `}</style>
    </>
  );
}

export default function Pagina() {
  return (
    <Suspense fallback={<p className="cargando">Cargando…</p>}>
      <Reporte />
    </Suspense>
  );
}
