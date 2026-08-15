"use client";

/**
 * La bandeja: los ficheros que la ingesta no supo asignar o fechar.
 *
 * Es lo que garantiza que ningún fichero se pierda en silencio. Al asignar se
 * puede recordar el alias del dispositivo, y entonces los próximos ficheros de
 * ese teléfono se resuelven solos: el trabajo es una vez por dispositivo, no
 * todos los días.
 */

import { useEffect, useState } from "react";
import { enviar, pedir } from "@/lib/api";

interface FicheroBandeja {
  ID: string;
  Nombre: string;
  Fuente: string;
  RutaCarpeta: string | null;
  Estado: string;
  Error: string | null;
  PistaAlias: string | null;
  Fecha: string | null;
  OrigenFecha: string;
  PuntosTotal: number;
  ImportadoAt: string;
}

interface Vendedor {
  ID: string;
  Nombre: string;
}

const MOTIVOS: Record<string, string> = {
  SIN_ASIGNAR: "No se supo de quién es",
  SIN_FECHA: "No se pudo fechar",
  ERROR: "No se pudo leer",
};

export default function Bandeja() {
  const [ficheros, setFicheros] = useState<FicheroBandeja[]>([]);
  const [vendedores, setVendedores] = useState<Vendedor[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [guardando, setGuardando] = useState<string | null>(null);

  async function cargar() {
    try {
      const [f, v] = await Promise.all([
        pedir<FicheroBandeja[]>("/api/bandeja"),
        pedir<Vendedor[]>("/api/vendedores"),
      ]);
      setFicheros(f ?? []);
      setVendedores(v ?? []);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  useEffect(() => {
    cargar();
  }, []);

  async function asignar(f: FicheroBandeja, vendedorId: string, fecha: string, recordar: boolean) {
    if (!vendedorId) return;
    setGuardando(f.ID);
    try {
      await enviar("/api/bandeja/asignar", {
        ficheroId: f.ID,
        vendedorId,
        fecha: fecha || null,
        recordarAlias: recordar,
        alias: f.PistaAlias ?? "",
      });
      await cargar();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setGuardando(null);
    }
  }

  return (
    <>
      <h1>Bandeja de entrada</h1>
      <p className="sub">
        Ficheros que llegaron pero no se pudieron colocar. Al asignarlos, marca
        «recordar» y los siguientes de ese mismo dispositivo se resolverán solos.
      </p>

      {error && <p className="aviso">{error}</p>}

      {ficheros.length === 0 && !error && (
        <div className="tarjeta">
          <p>Nada pendiente. Todos los ficheros se colocaron solos.</p>
        </div>
      )}

      {ficheros.map((f) => (
        <FilaBandeja
          key={f.ID}
          fichero={f}
          vendedores={vendedores}
          guardando={guardando === f.ID}
          onAsignar={asignar}
        />
      ))}
    </>
  );
}

function FilaBandeja({
  fichero,
  vendedores,
  guardando,
  onAsignar,
}: {
  fichero: FicheroBandeja;
  vendedores: Vendedor[];
  guardando: boolean;
  onAsignar: (f: FicheroBandeja, v: string, fecha: string, recordar: boolean) => void;
}) {
  const [vendedor, setVendedor] = useState("");
  const [fecha, setFecha] = useState(fichero.Fecha?.slice(0, 10) ?? "");
  const [recordar, setRecordar] = useState(true);

  return (
    <div className="tarjeta">
      <b>{fichero.Nombre}</b>
      <p className="sub">
        {MOTIVOS[fichero.Estado] ?? fichero.Estado} · carpeta{" "}
        {fichero.Fuente}
        {fichero.RutaCarpeta ? ` / ${fichero.RutaCarpeta}` : ""} ·{" "}
        {fichero.PuntosTotal} puntos
        {fichero.PistaAlias ? (
          <>
            {" "}
            · el fichero dice <b>{fichero.PistaAlias}</b>
          </>
        ) : null}
      </p>

      {fichero.Error && <p className="aviso">{fichero.Error}</p>}

      {fichero.Estado !== "ERROR" && (
        <div className="controles">
          <select value={vendedor} onChange={(e) => setVendedor(e.target.value)}>
            <option value="">¿De quién es?</option>
            {vendedores.map((v) => (
              <option key={v.ID} value={v.ID}>
                {v.Nombre}
              </option>
            ))}
          </select>

          <input
            type="date"
            value={fecha}
            onChange={(e) => setFecha(e.target.value)}
          />

          <label>
            <input
              type="checkbox"
              checked={recordar}
              onChange={(e) => setRecordar(e.target.checked)}
            />{" "}
            Recordar «{fichero.PistaAlias ?? fichero.Nombre}» para este vendedor
          </label>

          <button
            className="primario"
            disabled={!vendedor || guardando}
            onClick={() => onAsignar(fichero, vendedor, fecha, recordar)}
          >
            {guardando ? "Guardando…" : "Asignar"}
          </button>
        </div>
      )}
    </div>
  );
}
