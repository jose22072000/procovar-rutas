"use client";

/**
 * Administration: the Drive folders, the remembered aliases and the state of the
 * scans.
 */

import { useEffect, useState } from "react";
import { useEvents } from "@/lib/events";
import { enviar, ask, borrar } from "@/lib/api";

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

// Lo que contesta el barrido. Sin etiquetas json en el Go, así que llegan con el
// nombre del campo tal cual.
interface Resultado {
  Seen: number;
  New: number;
  Failed: number;
  Saltadas: number;
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
  const [resultado, setResultado] = useState<string | null>(null);

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

  // Quitar una carpeta de la lista. Se pregunta porque es de las cosas que se hacen
  // sin querer al ir a tocar la de al lado.
  async function quitar(f: Fuente) {
    if (!confirm(`¿Quitar la carpeta «${f.name}»?\n\nDeja de barrerse y de salir aquí. Las rutas que ya entraron por ella se quedan.`)) {
      return;
    }
    try {
      await borrar(`/api/sources/${f.id}`);
      await cargar();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function barrer(tipoBarrido: string) {
    setBarriendo(true);
    setError(null);
    setResultado(null);
    try {
      const r = await enviar<Resultado>(`/api/ingest/scan?type=${tipoBarrido}`, {});
      // Decir qué pasó, incluso cuando no pasó nada. Un botón que se pulsa y deja
      // la pantalla igual no se distingue de un botón roto.
      setResultado(
        r.Saltadas > 0 && r.Seen === 0
          ? `No se barrió ninguna carpeta (${r.Saltadas}): este servicio no lee Drive por su cuenta, los ficheros los trae n8n.`
          : `${r.Seen} ficheros vistos, ${r.New} nuevos, ${r.Failed} fallidos.`,
      );
      await cargar();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBarriendo(false);
    }
  }

  // Las carpetas, agrupadas por sucursal. Setenta seguidas son una lista que no se
  // lee; por sucursal se ve de un vistazo quién tiene cuántas y a quién le falta.
  const porSucursal = fuentes.reduce<Record<string, Fuente[]>>((acc, f) => {
    const s = f.branch || "sin sucursal";
    (acc[s] ??= []).push(f);
    return acc;
  }, {});

  return (
    <>
      <h1>Administración</h1>
      <p className="sub">Carpetas de Drive, alias de dispositivo y barridos.</p>

      {error && <p className="aviso">{error}</p>}

      <div className="tarjeta">
        <b>Carpetas de Drive</b>
        <p className="sub">
          El identificador sale de la URL de la carpeta:
          drive.google.com/drive/folders/<b>ESTE_TROZO</b>. Los ficheros los trae
          n8n con la cuenta padre, y al entrar los aparta a la subcarpeta «GPS
          Procesados» de cada vendedor: por eso una carpeta que lleva días sin
          barrerse no significa nada raro.
        </p>

        {Object.keys(porSucursal)
          .sort()
          .map((sucursal) => (
            <div key={sucursal} className="grupo-carpetas">
              <div className="pv-rotulo">
                {sucursal} · {porSucursal[sucursal].length}
              </div>
              {porSucursal[sucursal].map((f) => (
                <div className="dato" key={f.id}>
                  <span>{f.name}</span>
                  <span>
                    {f.lastError ? (
                      <span style={{ color: "var(--falta)" }}>{f.lastError}</span>
                    ) : f.lastScan ? (
                      new Date(f.lastScan).toLocaleString("es")
                    ) : (
                      "entra por n8n"
                    )}
                    <button
                      className="pv-boton"
                      style={{ marginLeft: "0.75rem" }}
                      onClick={() => quitar(f)}
                    >
                      Quitar
                    </button>
                  </span>
                </div>
              ))}
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
          ingesta. Si «pendientes» crece y no baja, el servicio de ingesta está
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
              <span>Apartados tras varios intentos</span>
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

        {resultado && <p className="sub">{resultado}</p>}

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
