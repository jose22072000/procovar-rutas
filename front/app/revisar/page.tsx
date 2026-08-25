"use client";

/**
 * Qué hay que revisar.
 *
 * # Por qué esto no está en el calendario
 *
 * El calendario es una cuadrícula de vendedores por días y se abre para una sola
 * cosa: ver de un vistazo quién trabajó y quién no. Poner ahí encima los treinta y
 * seis que llevan días sin subir, más los ficheros ilegibles con su error de XML,
 * lo convertía en un muro de texto que hay que atravesar para llegar a lo que se
 * venía a ver — y encima con el detalle a medias, porque ahí no cabe.
 *
 * Aquí sí cabe, y esto es lo que se necesita para ARREGLARLO: qué fichero falló, de
 * qué carpeta salió, de qué día es y qué dijo el error, para poder ir a subirlo otra
 * vez a mano. Quién lleva sin subir y desde cuándo. Y quién es quién con PEDIDO,
 * tanto lo que ya está atado como lo que falta.
 *
 * El calendario se queda con una línea que dice cuántas cosas hay y trae aquí.
 */

import { useCallback, useEffect, useState } from "react";
import SinPermiso from "@/components/SinPermiso";
import { useSesion } from "@/components/Sesion";
import { useEvents } from "@/lib/events";
import { ask, enviar, type Seller } from "@/lib/api";

interface FicheroAtascado {
  id: string;
  name: string;
  source: string;
  folderPath: string | null;
  seller: string;
  status: string;
  error: string | null;
  date: string | null;
  points: number;
  importedAt: string;
}

interface VendedorCallado {
  sellerId: string;
  seller: string;
  lastUpload: string | null;
  daysSilent: number;
  stuckFiles: number;
  linked: boolean;
}

interface Enlace {
  id: string;
  vendorCode: string;
  vendorName: string;
  sellerId: string;
  seller: string;
  origin: string;
}

interface Suelto {
  branchId: string;
  branch: string;
  vendorCode: string;
  vendorName: string;
  orders: number;
  lastOrder: string;
}

interface Alias {
  id: string;
  originalAlias: string;
  seller: string;
}

interface Revision {
  files: FicheroAtascado[];
  silent: VendedorCallado[];
  links: Enlace[];
  unlinked: Suelto[];
  daysMissing?: number;
  queue?: { pendientes: number; haciendose: number; apartados: number };
}

const MOTIVOS: Record<string, string> = {
  SIN_ASIGNAR: "No se supo de quién es",
  SIN_FECHA: "No se pudo fechar",
  ERROR: "No se pudo leer",
};

// Qué hacer con cada motivo. Un error de XML no se arregla desde aquí —hay que
// volver a subir el fichero—, pero uno sin dueño sí.
const QUE_HACER: Record<string, string> = {
  SIN_ASIGNAR: "Dile de quién es y el dispositivo se recuerda para siempre.",
  SIN_FECHA: "Ponle la fecha del día que cubre.",
  ERROR:
    "El fichero está roto o cortado: hay que volver a subirlo a Drive desde el teléfono. " +
    "Aquí no hay nada que asignar.",
};

export default function Revisar() {
  const { cargando, vetado, puede } = useSesion();
  const [datos, setDatos] = useState<Revision | null>(null);
  const [vendedores, setVendedores] = useState<Seller[]>([]);
  const [alias, setAlias] = useState<Alias[]>([]);
  const [error, setError] = useState<string | null>(null);

  const cargar = useCallback(() => {
    ask<Revision>("/api/review")
      .then(setDatos)
      .catch((e) => setError((e as Error).message));
    ask<Seller[]>("/api/sellers")
      .then((v) => setVendedores(v ?? []))
      .catch(() => setVendedores([]));
    ask<Alias[]>("/api/aliases")
      .then((a) => setAlias(a ?? []))
      .catch(() => setAlias([]));
  }, []);

  useEffect(cargar, [cargar]);

  // En vivo: el trabajador va trayendo días y la ingesta va colocando ficheros
  // mientras esta pantalla está abierta. Sin esto habría que recargar para saber si
  // algo avanzó, que es justo la pregunta que se viene a hacer aquí.
  useEvents(["file", "day", "pedidos", "scan"], cargar);

  if (cargando) return <p className="cargando">Cargando…</p>;
  if (vetado) return <SinPermiso que="Rutas" detalle={vetado.replace("sin permiso: ", "")} />;
  if (!puede("rutas.calendario")) return <SinPermiso que="esta pantalla" detalle="rutas.calendario" />;

  if (error) return <p className="aviso">{error}</p>;
  if (!datos) return <p className="cargando">Mirando qué falta…</p>;

  const rotos = datos.files.filter((f) => f.status === "ERROR");
  const colocables = datos.files.filter((f) => f.status !== "ERROR");
  const callados = datos.silent
    .filter((s) => s.daysSilent === -1 || s.daysSilent > 3)
    .sort((a, b) => (b.daysSilent < 0 ? 1e9 : b.daysSilent) - (a.daysSilent < 0 ? 1e9 : a.daysSilent));

  return (
    <>
      <h1>Qué hay que revisar</h1>
      <p className="sub">
        Lo que impide que el calendario esté completo, con el detalle para poder
        arreglarlo.
      </p>

      {/* Cómo va el trabajador de pedidos. Va primero porque explica por qué una
          pantalla puede estar a medio llenar: no está rota, se está poniendo al día. */}
      {(datos.daysMissing ?? 0) > 0 && (
        <div className="tarjeta">
          <b>Trayendo los clientes del histórico</b>
          <p className="sub">
            Quedan <b>{datos.daysMissing}</b> días de los que ya hay ruta y todavía no
            se han pedido sus clientes a PEDIDO. Se traen de treinta en treinta y con
            una pausa entre uno y otro para no cargar a PEDIDO, así que va lento a
            propósito: son días que ya pasaron.
            {datos.queue
              ? ` Ahora mismo hay ${datos.queue.pendientes} esperando en la cola.`
              : ""}
          </p>
        </div>
      )}

      {/* Los rotos, primero: son los que solo se arreglan volviendo a subir. */}
      {rotos.length > 0 && (
        <div className="tarjeta">
          <b>
            {rotos.length === 1
              ? "Un fichero llegó roto"
              : `${rotos.length} ficheros llegaron rotos`}
          </b>
          <p className="sub">
            Llegaron a Drive, pero el .gpx está cortado o mal formado y no se pudo
            leer. Hay que volver a subirlos desde el teléfono: aquí no hay nada que
            asignar. La fecha del nombre dice qué día se perdió.
          </p>
          {rotos.map((f) => (
            <div className="fila-suelta" key={f.id}>
              <div>
                <b>{f.name}</b>
                <span className="sub">
                  carpeta {f.source}
                  {f.folderPath ? ` / ${f.folderPath}` : ""}
                  {f.seller ? ` · ${f.seller}` : ""}
                  {f.date ? ` · del ${f.date}` : ""} · {f.points} puntos
                </span>
                {f.error && <span className="aviso">{f.error}</span>}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Y los que sí se pueden colocar desde aquí. */}
      {colocables.length > 0 && (
        <div className="tarjeta">
          <b>
            {colocables.length === 1
              ? "Un fichero llegó y no se pudo colocar"
              : `${colocables.length} ficheros llegaron y no se pudieron colocar`}
          </b>
          <p className="sub">
            Al asignarlos se recuerda el dispositivo, y los siguientes de ese mismo
            teléfono se colocan solos. Es una vez por dispositivo, no todos los días.
          </p>
          {colocables.map((f) => (
            <FilaFichero key={f.id} fichero={f} vendedores={vendedores} alAsignar={cargar} />
          ))}
        </div>
      )}

      {/* Quién no está subiendo, CON FECHA. */}
      <div className="tarjeta">
        <b>
          {callados.length === 0
            ? "Todos están subiendo"
            : callados.length === 1
              ? "Un vendedor lleva días sin subir"
              : `${callados.length} vendedores llevan días sin subir`}
        </b>
        <p className="sub">
          O no llevan el GPS encendido, o dejó de subir. Más de 3 días callado es un
          teléfono que hay que ir a mirar.
        </p>
        {callados.length > 0 && (
          <table className="movements">
            <thead>
              <tr>
                <th>Vendedor</th>
                <th>Última ruta suya</th>
                <th>Lleva</th>
                <th>Ficheros atascados</th>
                <th>Vendedor de PEDIDO</th>
              </tr>
            </thead>
            <tbody>
              {callados.map((c) => (
                <tr key={c.sellerId}>
                  <td>{c.seller}</td>
                  <td className="pv-codigo">{c.lastUpload ?? "nunca ha subido"}</td>
                  <td>{c.daysSilent < 0 ? "—" : `${c.daysSilent} días`}</td>
                  <td>{c.stuckFiles > 0 ? c.stuckFiles : "—"}</td>
                  <td>
                    {c.linked ? (
                      "emparejado"
                    ) : (
                      <span style={{ color: "var(--pv-cuno)" }}>sin emparejar</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* De quién es cada GPS. */}
      {puede("rutas.alias") && (
        <div className="tarjeta">
          <b>De quién es cada GPS</b>
          <p className="sub">
            Un .gpx no dice de quién es: lo único que trae es el nombre del perfil del
            GPS, que es el de su carpeta en Drive («GPS Diana Acosta», «STGGari»,
            «TABLET3»). Aquí se dice a quién pertenece cada uno, y a partir de ahí sus
            ficheros entran solos.
          </p>
          <p className="sub">
            Cuando se entregue un teléfono nuevo, apúntalo aquí ANTES de que suba nada:
            si no, sus primeros días se quedan esperando arriba, en «no se pudo
            colocar».
          </p>

          <NuevoAlias vendedores={vendedores} alGuardar={cargar} />

          {alias.length === 0 ? (
            <p className="sub">Todavía no hay ninguno apuntado.</p>
          ) : (
            <table className="movements">
              <thead>
                <tr>
                  <th>GPS / carpeta</th>
                  <th>Es de</th>
                </tr>
              </thead>
              <tbody>
                {alias.map((a) => (
                  <tr key={a.id}>
                    <td className="pv-codigo">{a.originalAlias}</td>
                    <td>{a.seller}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Quién es quién con PEDIDO: lo que falta, y lo que ya está. */}
      {puede("rutas.alias") && (
        <div className="tarjeta">
          <b>Vendedores de PEDIDO</b>
          <p className="sub">
            Sus nombres nacen en otro sitio —el maestro de vendedores— y aquí nacen del
            nombre de una carpeta de Drive, así que no coinciden. Lo que no tiene duda
            se empareja solo; lo que le vale a dos, no se empareja y se pregunta. Es
            una vez por vendedor.
          </p>

          {datos.unlinked.length === 0 ? (
            <p className="sub">No falta ninguno por emparejar.</p>
          ) : (
            datos.unlinked.map((v) => (
              <FilaVendedor
                key={`${v.branchId}:${v.vendorCode}`}
                vendedor={v}
                vendedores={vendedores}
                alEmparejar={cargar}
              />
            ))
          )}

          {datos.links.length > 0 && (
            <>
              <div className="pv-rotulo" style={{ marginTop: "1rem" }}>
                Ya emparejados · {datos.links.length}
              </div>
              <table className="movements">
                <thead>
                  <tr>
                    <th>En PEDIDO</th>
                    <th>Código</th>
                    <th>Es, aquí</th>
                    <th>Lo dijo</th>
                  </tr>
                </thead>
                <tbody>
                  {datos.links.map((l) => (
                    <tr key={l.id}>
                      <td>{l.vendorName}</td>
                      <td className="pv-codigo">{l.vendorCode}</td>
                      <td>{l.seller}</td>
                      {/* De dónde salió el emparejamiento: si fue el automático, es
                          revisable; si lo dijo una persona, no se toca. */}
                      <td>{l.origin === "manual" ? "una persona" : "el nombre"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </div>
      )}
    </>
  );
}

function FilaFichero({
  fichero,
  vendedores,
  alAsignar,
}: {
  fichero: FicheroAtascado;
  vendedores: Seller[];
  alAsignar: () => void;
}) {
  const [seller, setVendedor] = useState("");
  const [fecha, setFecha] = useState(fichero.date ?? "");
  const [guardando, setGuardando] = useState(false);
  const [fallo, setFallo] = useState<string | null>(null);

  async function asignar() {
    if (!seller) return;
    setGuardando(true);
    setFallo(null);
    try {
      await enviar("/api/inbox/assign", {
        fileId: fichero.id,
        sellerId: seller,
        fecha: fecha || null,
        rememberAlias: true,
        alias: "",
      });
      alAsignar();
    } catch (e) {
      setFallo((e as Error).message);
    } finally {
      setGuardando(false);
    }
  }

  return (
    <div className="fila-suelta">
      <div>
        <b>{fichero.name}</b>
        <span className="sub">
          {MOTIVOS[fichero.status] ?? fichero.status} · carpeta {fichero.source}
          {fichero.folderPath ? ` / ${fichero.folderPath}` : ""} · {fichero.points} puntos
        </span>
        <span className="sub">{QUE_HACER[fichero.status] ?? ""}</span>
        {fichero.error && <span className="aviso">{fichero.error}</span>}
      </div>
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
          className="pv-campo"
          type="date"
          value={fecha}
          onChange={(e) => setFecha(e.target.value)}
        />
        <button
          className="pv-boton pv-boton-primario"
          disabled={!seller || guardando}
          onClick={asignar}
        >
          {guardando ? "Guardando…" : "Asignar"}
        </button>
      </div>
      {fallo && <p className="aviso">{fallo}</p>}
    </div>
  );
}

function FilaVendedor({
  vendedor,
  vendedores,
  alEmparejar,
}: {
  vendedor: Suelto;
  vendedores: Seller[];
  alEmparejar: () => void;
}) {
  const [seller, setVendedor] = useState("");
  const [guardando, setGuardando] = useState(false);
  const [fallo, setFallo] = useState<string | null>(null);

  async function emparejar() {
    if (!seller) return;
    setGuardando(true);
    setFallo(null);
    try {
      await enviar("/api/pedidos/emparejar", {
        vendorCode: vendedor.vendorCode,
        vendorName: vendedor.vendorName,
        sellerId: seller,
      });
      alEmparejar();
    } catch (e) {
      setFallo((e as Error).message);
    } finally {
      setGuardando(false);
    }
  }

  return (
    <div className="fila-suelta">
      <div>
        <b>{vendedor.vendorName}</b>
        <span className="sub">
          <span className="pv-codigo">{vendedor.vendorCode}</span> · {vendedor.branch} ·{" "}
          {vendedor.orders} {vendedor.orders === 1 ? "pedido" : "pedidos"} · el último
          el {vendedor.lastOrder}
        </span>
      </div>
      <div className="controles">
        <select value={seller} onChange={(e) => setVendedor(e.target.value)}>
          <option value="">¿Quién es aquí?</option>
          {vendedores.map((v) => (
            <option key={v.id} value={v.id}>
              {v.name}
            </option>
          ))}
        </select>
        <button
          className="pv-boton pv-boton-primario"
          disabled={!seller || guardando}
          onClick={emparejar}
        >
          {guardando ? "Guardando…" : "Es este"}
        </button>
      </div>
      {fallo && <p className="aviso">{fallo}</p>}
    </div>
  );
}

function NuevoAlias({
  vendedores,
  alGuardar,
}: {
  vendedores: Seller[];
  alGuardar: () => void;
}) {
  const [alias, setAlias] = useState("");
  const [seller, setVendedor] = useState("");
  const [guardando, setGuardando] = useState(false);
  const [fallo, setFallo] = useState<string | null>(null);

  async function guardar() {
    if (!alias.trim() || !seller) return;
    setGuardando(true);
    setFallo(null);
    try {
      await enviar("/api/aliases", { alias: alias.trim(), sellerId: seller });
      setAlias("");
      setVendedor("");
      alGuardar();
    } catch (e) {
      setFallo((e as Error).message);
    } finally {
      setGuardando(false);
    }
  }

  return (
    <>
      <div className="controles">
        <input
          className="pv-campo"
          placeholder="Nombre del GPS o de su carpeta"
          value={alias}
          onChange={(e) => setAlias(e.target.value)}
          style={{ minWidth: 260 }}
        />
        <select value={seller} onChange={(e) => setVendedor(e.target.value)}>
          <option value="">¿De quién es?</option>
          {vendedores.map((v) => (
            <option key={v.id} value={v.id}>
              {v.name}
            </option>
          ))}
        </select>
        <button
          className="pv-boton pv-boton-primario"
          disabled={!alias.trim() || !seller || guardando}
          onClick={guardar}
        >
          {guardando ? "Guardando…" : "Apuntar"}
        </button>
      </div>
      {fallo && <p className="aviso">{fallo}</p>}
    </>
  );
}
