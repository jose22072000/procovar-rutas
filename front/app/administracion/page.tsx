"use client";

/**
 * Administración: las carpetas de Drive, los alias memorizados y el estado de
 * los barridos.
 */

import { useEffect, useState } from "react";
import { enviar, pedir } from "@/lib/api";

interface Fuente {
  ID: string;
  Nombre: string;
  FolderID: string;
  Tipo: string;
  Activa: boolean;
  UltimoBarrido: string | null;
  UltimoError: string | null;
}

interface Alias {
  ID: string;
  AliasOriginal: string;
  Trabajador: string;
}

interface EstadoCola {
  activa: boolean;
  pendientes?: number;
  procesando?: number;
  fallidos?: number;
}

interface Barrido {
  ID: string;
  Tipo: string;
  Inicio: string;
  Fin: string | null;
  FicherosVistos: number;
  FicherosNuevos: number;
  FicherosError: number;
  PuntosInsertados: number;
  Ok: boolean;
  Detalle: string | null;
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
        pedir<Fuente[]>("/api/fuentes"),
        pedir<Alias[]>("/api/alias"),
        pedir<Barrido[]>("/api/barridos"),
        pedir<EstadoCola>("/api/cola"),
      ]);
      setFuentes(f ?? []);
      setAlias(a ?? []);
      setBarridos(b ?? []);
      setCola(c ?? null);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  useEffect(() => {
    cargar();
  }, []);

  async function crear() {
    if (!nombre || !folderId) return;
    try {
      await enviar("/api/fuentes", { nombre, folderId, tipo });
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
      await enviar(`/api/ingesta/barrer?tipo=${tipoBarrido}`, {});
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
          <div className="dato" key={f.ID}>
            <span>
              {f.Nombre} <small style={{ color: "var(--tenue)" }}>({f.Tipo})</small>
            </span>
            <span>
              {f.UltimoError ? (
                <span style={{ color: "var(--falta)" }}>{f.UltimoError}</span>
              ) : f.UltimoBarrido ? (
                new Date(f.UltimoBarrido).toLocaleString("es")
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
          <button className="primario" onClick={crear}>
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
        {cola?.activa ? (
          <>
            <div className="dato">
              <span>Pendientes</span>
              <span>{cola.pendientes}</span>
            </div>
            <div className="dato">
              <span>Procesándose</span>
              <span>{cola.procesando}</span>
            </div>
            <div className="dato">
              <span>Apartados tras varios intentos</span>
              <span
                style={{ color: (cola.fallidos ?? 0) > 0 ? "var(--falta)" : undefined }}
              >
                {cola.fallidos}
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

        <table className="movimientos">
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
              <tr key={b.ID}>
                <td>{new Date(b.Inicio).toLocaleString("es")}</td>
                <td>{b.Tipo}</td>
                <td>{b.FicherosVistos}</td>
                <td>{b.FicherosNuevos}</td>
                <td>{b.FicherosError}</td>
                <td>{b.PuntosInsertados}</td>
                <td>{b.Ok ? "ok" : (b.Detalle ?? "falló")}</td>
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
            <div className="dato" key={a.ID}>
              <span>{a.AliasOriginal}</span>
              <span>{a.Trabajador}</span>
            </div>
          ))
        )}
      </div>
    </>
  );
}
