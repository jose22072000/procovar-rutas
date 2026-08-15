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
import { useEventos } from "@/lib/eventos";
import { enviar, pedir } from "@/lib/api";

interface FicheroBandeja {
  id: string;
  name: string;
  source: string;
  folderPath: string | null;
  status: string;
  error: string | null;
  aliasHint: string | null;
  date: string | null;
  dateSource: string;
  points: number;
  importedAt: string;
}

interface Vendedor {
  id: string;
  name: string;
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
        pedir<FicheroBandeja[]>("/api/inbox"),
        pedir<Vendedor[]>("/api/sellers"),
      ]);
      setFicheros(f ?? []);
      setVendedores(v ?? []);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  // Un fichero nuevo, o uno que otra persona acaba de asignar.
  useEventos(["file"], () => {
    void cargar();
  });

  useEffect(() => {
    cargar();
  }, []);

  async function asignar(f: FicheroBandeja, sellerId: string, fecha: string, recordar: boolean) {
    if (!sellerId) return;
    setGuardando(f.id);
    try {
      await enviar("/api/inbox/assign", {
        fileId: f.id,
        sellerId,
        fecha: fecha || null,
        rememberAlias: recordar,
        alias: f.aliasHint ?? "",
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
          key={f.id}
          fichero={f}
          vendedores={vendedores}
          guardando={guardando === f.id}
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
  const [seller, setVendedor] = useState("");
  const [fecha, setFecha] = useState(fichero.date?.slice(0, 10) ?? "");
  const [recordar, setRecordar] = useState(true);

  return (
    <div className="tarjeta">
      <b>{fichero.name}</b>
      <p className="sub">
        {MOTIVOS[fichero.status] ?? fichero.status} · carpeta{" "}
        {fichero.source}
        {fichero.folderPath ? ` / ${fichero.folderPath}` : ""} ·{" "}
        {fichero.points} points
        {fichero.aliasHint ? (
          <>
            {" "}
            · el fichero dice <b>{fichero.aliasHint}</b>
          </>
        ) : null}
      </p>

      {fichero.error && <p className="aviso">{fichero.error}</p>}

      {fichero.status !== "ERROR" && (
        <div className="controles">
          <select value={seller} onChange={(e) => setVendedor(e.target.value)}>
            <option value="">¿De quién es?</option>
            {vendedores.map((v) => (
              <option key={v.id} value={v.id}>
                {v.name}
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
            Recordar «{fichero.aliasHint ?? fichero.name}» para este seller
          </label>

          <button
            className="primario"
            disabled={!seller || guardando}
            onClick={() => onAsignar(fichero, seller, fecha, recordar)}
          >
            {guardando ? "Guardando…" : "Asignar"}
          </button>
        </div>
      )}
    </div>
  );
}
