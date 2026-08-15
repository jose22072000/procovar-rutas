"use client";

/**
 * El reporte semanal por vendedor: de lunes a viernes, con cada movimiento
 * detallado entre las 9:00 y las 16:00.
 *
 * Se imprime a PDF desde el propio navegador (Ctrl+P → Guardar como PDF). No hay
 * generador de PDF en el servidor a propósito: el navegador ya sabe paginar,
 * y una segunda maquetación en el backend sería otra cosa más que mantener
 * sincronizada con esta.
 */

import { Suspense, useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { pedir } from "@/lib/api";

interface Movimiento {
  tipo: string;
  horaInicio: string;
  horaFin: string;
  duracionMin: number;
  distanciaKm: number;
  velMedia: number;
  velMaxima: number;
  lugar?: string;
}

interface DiaReporte {
  fecha: string;
  estado: string;
  motivo?: string;
  primerFix?: string;
  ultimoFix?: string;
  kmNetos: number;
  cobertura: number;
  minParado: number;
  minMovimiento: number;
  banderas: string[];
  movimientos: Movimiento[];
  lugar?: string;
}

interface Documento {
  cabecera: {
    vendedor: string;
    desde: string;
    hasta: string;
    jornada: string;
  };
  resumen: {
    diasOk: number;
    diasSinFichero: number;
    diasSinFecha: number;
    diasSinMovimiento: number;
    kmTotal: number;
    paradas: number;
    coberturaMedia: number;
  };
  dias: DiaReporte[];
}

function Reporte() {
  const params = useSearchParams();
  const vendedor = params.get("vendedor") ?? "";
  const fecha = params.get("fecha") ?? "";
  const [doc, setDoc] = useState<Documento | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!vendedor || !fecha) return;
    pedir<Documento>(`/api/reporte/semanal?vendedor=${vendedor}&fecha=${fecha}`)
      .then(setDoc)
      .catch((e) => setError(e.message));
  }, [vendedor, fecha]);

  if (error) return <p className="aviso">{error}</p>;
  if (!doc) return <p className="cargando">Armando el reporte…</p>;

  const incidencias =
    doc.resumen.diasSinFichero +
    doc.resumen.diasSinFecha +
    doc.resumen.diasSinMovimiento;

  return (
    <>
      <div className="controles no-imprimir">
        <button className="primario" onClick={() => window.print()}>
          Imprimir / Guardar como PDF
        </button>
      </div>

      <h1>{doc.cabecera.vendedor}</h1>
      <p className="sub">
        Semana del {doc.cabecera.desde} al {doc.cabecera.hasta} · jornada{" "}
        {doc.cabecera.jornada}
      </p>

      <div className="tarjeta">
        <b>Resumen de la semana</b>
        <div className="dato">
          <span>Días trabajados</span>
          <span>{doc.resumen.diasOk} de 5</span>
        </div>
        <div className="dato">
          <span>Incidencias</span>
          <span>
            {incidencias === 0 ? "ninguna" : incidencias}
            {incidencias > 0 && (
              <>
                {" "}
                ({doc.resumen.diasSinFichero} sin fichero,{" "}
                {doc.resumen.diasSinFecha} sin fecha,{" "}
                {doc.resumen.diasSinMovimiento} sin moverse)
              </>
            )}
          </span>
        </div>
        <div className="dato">
          <span>Kilómetros</span>
          <span>{doc.resumen.kmTotal.toFixed(2)} km</span>
        </div>
        <div className="dato">
          <span>Paradas</span>
          <span>{doc.resumen.paradas}</span>
        </div>
        <div className="dato">
          <span>Cobertura media</span>
          <span>{doc.resumen.coberturaMedia.toFixed(0)} %</span>
        </div>
      </div>

      {doc.dias.map((d) => (
        <div className="tarjeta" key={d.fecha}>
          <b>{d.fecha}</b>
          {d.motivo && <p className="sub">{d.motivo}</p>}

          {d.movimientos.length > 0 ? (
            <>
              <p className="sub">
                {d.primerFix}–{d.ultimoFix} · {d.kmNetos.toFixed(2)} km ·{" "}
                {d.cobertura.toFixed(0)} % de cobertura · {d.minMovimiento} min
                en movimiento, {d.minParado} min parado
              </p>
              <table className="movimientos">
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
                  {d.movimientos.map((m, i) => (
                    <tr key={i} className={m.tipo === "parada" ? "parada" : ""}>
                      <td>{m.horaInicio}</td>
                      <td>{m.horaFin}</td>
                      <td>{m.duracionMin} min</td>
                      <td>
                        {m.tipo === "parada" ? <b>Parada</b> : "Desplazamiento"}
                      </td>
                      <td>{m.tipo === "parada" ? "—" : `${m.distanciaKm} km`}</td>
                      <td>{m.tipo === "parada" ? "—" : `${m.velMedia} km/h`}</td>
                      <td>{m.tipo === "parada" ? "—" : `${m.velMaxima} km/h`}</td>
                      <td>{m.lugar ?? "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : (
            d.lugar && <p className="sub">Estuvo en {d.lugar}.</p>
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
