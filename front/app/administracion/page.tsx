"use client";

/**
 * Administration: the Drive folders, the remembered aliases and the state of the
 * scans.
 */

import { useEffect, useState } from "react";
import { useEvents } from "@/lib/events";
import { enviar, ask } from "@/lib/api";

interface Fuente {
  id: string;
  name: string;
  branch: string;
  folderId: string;
  type: string;
  active: boolean;
  hasCredential: boolean;
  lastScan: string | null;
  lastError: string | null;
}

interface Alias {
  id: string;
  originalAlias: string;
  seller: string;
}

interface EstadoCola {
  active: boolean;
  pending?: number;
  processing?: number;
  failed?: number;
}

interface Barrido {
  id: string;
  type: string;
  start: string;
  end: string | null;
  filesSeen: number;
  filesNew: number;
  filesFailed: number;
  pointsInserted: number;
  ok: boolean;
  detail: string | null;
}

export default function Administracion() {
  const [fuentes, setFuentes] = useState<Fuente[]>([]);
  const [alias, setAlias] = useState<Alias[]>([]);
  const [barridos, setBarridos] = useState<Barrido[]>([]);
  const [cola, setCola] = useState<EstadoCola | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [barriendo, setBarriendo] = useState(false);

  const [nombre, setNombre] = useState("");
  const [folderId, setFolderId] = useState("");
  const [tipo, setTipo] = useState("SUCURSAL");

  async function cargar() {
    try {
      const [f, a, b, c] = await Promise.all([
        ask<Fuente[]>("/api/sources"),
        ask<Alias[]>("/api/aliases"),
        ask<Barrido[]>("/api/scans"),
        ask<EstadoCola>("/api/queue"),
      ]);
      setFuentes(f ?? []);
      setAlias(a ?? []);
      setBarridos(b ?? []);
      setCola(c ?? null);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  // The queue and the scans move on their own: let the screen follow without a reload.
  useEvents(["queue", "file", "scan"], () => {
    void cargar();
  });

  useEffect(() => {
    cargar();
  }, []);

  async function crear() {
    if (!nombre || !folderId) return;
    try {
      await enviar("/api/sources", { nombre, folderId, tipo });
      setNombre("");
      setFolderId("");
      await cargar();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function barrer(tipoBarrido: string) {
    setBarriendo(true);
    setError(null);
    try {
      await enviar(`/api/ingest/scan?type=${tipoBarrido}`, {});
      await cargar();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBarriendo(false);
    }
  }

  return (
    <>
      <h1>Administración</h1>
      <p className="sub">Carpetas de Drive, alias de dispositivo y barridos.</p>

      {error && <p className="aviso">{error}</p>}

      <div className="tarjeta">
        <b>Carpetas de Drive</b>
        <p className="sub">
          El identificador sale de la URL de la carpeta:
          drive.google.com/drive/folders/<b>ESTE_TROZO</b>. Todas se leen con la
          misma cuenta de servicio, y siempre en modo lectura: este sistema nunca
          mueve ni borra nada del Drive.
        </p>

        {fuentes.map((f) => (
          <div className="dato" key={f.id}>
            <span>
              {f.name}{" "}
              {/* La sucursal a la que pertenece la carpeta: es lo que hay que
                  mirar para saber si la ingesta repartió cada una donde tocaba. */}
              <span className={f.branch ? "pv-etiqueta pv-etiqueta-azul" : "pv-etiqueta pv-etiqueta-cuno"}>
                {f.branch || "sin sucursal"}
              </span>
            </span>
            <span>
              {f.lastError ? (
                <span style={{ color: "var(--falta)" }}>{f.lastError}</span>
              ) : f.lastScan ? (
                new Date(f.lastScan).toLocaleString("es")
              ) : (
                "sin barrer todavía"
              )}
            </span>
          </div>
        ))}

        <div className="controles" style={{ marginTop: "1rem" }}>
          <input
            placeholder="Nombre (p. ej. Rutas Camagüey)"
            value={nombre}
            onChange={(e) => setNombre(e.target.value)}
          />
          <input
            placeholder="Identificador de la carpeta"
            value={folderId}
            onChange={(e) => setFolderId(e.target.value)}
            style={{ minWidth: 320 }}
          />
          <select value={tipo} onChange={(e) => setTipo(e.target.value)}>
            <option value="SUCURSAL">Carpeta de una sucursal</option>
            <option value="VENDEDOR">Carpeta de un solo vendedor</option>
            <option value="MIXTA">Mezclada</option>
          </select>
          <button className="pv-boton pv-boton-primario" onClick={crear}>
            Añadir carpeta
          </button>
        </div>
      </div>

      <div className="tarjeta">
        <b>Cola de n8n</b>
        <p className="sub">
          Los ficheros que empuja n8n esperan aquí y los procesa el servicio de
          ingesta. Si «pending» crece y no baja, el servicio de ingesta está
          parado.
        </p>
        {cola?.active ? (
          <>
            <div className="dato">
              <span>Pendientes</span>
              <span>{cola.pending}</span>
            </div>
            <div className="dato">
              <span>Procesándose</span>
              <span>{cola.processing}</span>
            </div>
            <div className="dato">
              <span>Apartados tras varios attempts</span>
              <span
                style={{ color: (cola.failed ?? 0) > 0 ? "var(--falta)" : undefined }}
              >
                {cola.failed}
              </span>
            </div>
          </>
        ) : (
          <p className="sub">
            Sin Redis: lo que empuje n8n se procesará en el acto, sin cola.
          </p>
        )}
      </div>

      <div className="tarjeta">
        <b>Barridos</b>
        <div className="controles">
          <button disabled={barriendo} onClick={() => barrer("manual")}>
            {barriendo ? "Barriendo…" : "Barrer ahora (incremental)"}
          </button>
          <button disabled={barriendo} onClick={() => barrer("nocturno")}>
            Repaso completo
          </button>
          <button disabled={barriendo} onClick={() => barrer("backfill")}>
            Traer todo el histórico
          </button>
        </div>

        <table className="movements">
          <thead>
            <tr>
              <th>Cuándo</th>
              <th>Tipo</th>
              <th>Vistos</th>
              <th>Nuevos</th>
              <th>Errores</th>
              <th>Puntos</th>
              <th>Estado</th>
            </tr>
          </thead>
          <tbody>
            {barridos.slice(0, 15).map((b) => (
              <tr key={b.id}>
                <td>{new Date(b.start).toLocaleString("es")}</td>
                <td>{b.type}</td>
                <td>{b.filesSeen}</td>
                <td>{b.filesNew}</td>
                <td>{b.filesFailed}</td>
                <td>{b.pointsInserted}</td>
                <td>{b.ok ? "ok" : (b.detail ?? "falló")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="tarjeta">
        <b>Alias de dispositivo</b>
        <p className="sub">
          Cada alias memorizado ahorra una asignación manual en la bandeja.
        </p>
        {alias.length === 0 ? (
          <p className="sub">Todavía no hay ninguno.</p>
        ) : (
          alias.map((a) => (
            <div className="dato" key={a.id}>
              <span>{a.originalAlias}</span>
              <span>{a.seller}</span>
            </div>
          ))
        )}
      </div>
    </>
  );
}
