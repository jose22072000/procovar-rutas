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
import { agruparPorSucursal, haceFaltaAgrupar } from "@/lib/porSucursal";
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

/** Un vendedor de PEDIDO y quién es aquí, si ya se dijo. */
interface Vendedor {
  ref: string;
  code: string;
  name: string;
  branch: string;
  orders: number;
  /** Vacío = todavía no se sabe quién es aquí. */
  sellerId: string;
  seller: string;
  origin: string;
}

/** Alguien de Accesos: una cuenta de verdad, con su rol. */
interface Persona {
  authUserId: string;
  name: string;
  email: string;
  branch: string;
  roles: string[];
  /** Si ya tiene ficha aquí. */
  sellerId: string;
}

/** Una carpeta de Drive, que es un teléfono. */
interface Carpeta {
  id: string;
  name: string;
  folderId: string;
  type: string;
  branch: string;
  sellerId: string;
  seller: string;
  files: number;
  lastFile: string;
  daysSilent: number;
  lastError: string;
}

interface DiaCortado {
  sellerId: string;
  seller: string;
  date: string;
  file: string;
  detail: string;
  points: number;
}

interface Revision {
  files: FicheroAtascado[];
  truncated?: DiaCortado[];
  silent: VendedorCallado[];
  vendors?: Vendedor[];
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
  const [carpetas, setCarpetas] = useState<Carpeta[]>([]);
  const [personas, setPersonas] = useState<Persona[]>([]);
  // Por qué no hay carpetas: «no hay ninguna» y «no se pudieron traer» se veían igual
  // —la pantalla vacía— y son dos problemas distintos.
  const [sinCarpetas, setSinCarpetas] = useState<string | null>(null);
  const [sinAccesos, setSinAccesos] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const cargar = useCallback(() => {
    ask<Revision>("/api/review")
      .then(setDatos)
      .catch((e) => setError((e as Error).message));
    ask<Seller[]>("/api/sellers")
      .then((v) => setVendedores(v ?? []))
      .catch(() => setVendedores([]));
    ask<Carpeta[]>("/api/gps")
      .then((g) => { setCarpetas(g ?? []); setSinCarpetas(null); })
      .catch((e) => { setCarpetas([]); setSinCarpetas((e as Error).message); });
    // Las personas salen de Accesos, que es donde están las cuentas de verdad. Si no
    // contesta se dice, porque sin esta lista no se puede asignar ningún GPS y hay
    // que saber que el problema no es la pantalla.
    ask<Persona[]>("/api/personas")
      .then((p) => {
        setPersonas(p ?? []);
        setSinAccesos(null);
      })
      .catch((e) => setSinAccesos((e as Error).message));
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
  const sinDuenno = (datos.vendors ?? []).filter((v) => v.sellerId === "");
  const sinDueno = carpetas.filter((g) => g.sellerId === "");

  /**
   * Las dos tablas de emparejar, SEPARADAS POR SUCURSAL.
   *
   * Salían planas con la sucursal en una columna. Emparejar es un trabajo que se hace por
   * sucursal —«voy a colocar los GPS de Camagüey»— y en una lista de ochenta y dos filas
   * ordenadas por otra cosa hay que ir saltando de una a otra leyendo esa columna.
   */
  const carpetasPorSucursal = agruparPorSucursal(carpetas, (g) => g.branch, (g) => g.name);
  const vendedoresPorSucursal = agruparPorSucursal(datos.vendors ?? [], (v) => v.branch, (v) => v.name);
  const agruparCarpetas = haceFaltaAgrupar(carpetasPorSucursal);
  const agruparVendedores = haceFaltaAgrupar(vendedoresPorSucursal);
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

      {/* Los días que entraron a medias. Van arriba porque son los que ENGAÑAN: el
          calendario los pinta en verde y con sus kilómetros, y no hay forma de saber
          desde ahí que son los de medio día. Los otros avisos se ven solos; este no. */}
      {(datos.truncated?.length ?? 0) > 0 && (
        <div className="tarjeta">
          <b>
            {datos.truncated!.length === 1
              ? "Un día entró a medias"
              : `${datos.truncated!.length} días entraron a medias`}
          </b>
          <p className="sub">
            Su fichero se cortó —el teléfono se quedó sin batería, lo mataron a media
            escritura o la subida a Drive se interrumpió— y lo que hay es el trozo que
            se pudo leer. <b>Los kilómetros de esos días NO son los de la jornada</b>,
            así que no sirven para juzgar a nadie. Si hace falta el día completo, hay
            que volver a subir ese .gpx.
          </p>
          <div className="tabla-ancha">
            <table className="movements">
              <thead>
                <tr>
                  <th>Día</th>
                  <th>Vendedor</th>
                  <th>Fichero</th>
                  <th>Puntos que entraron</th>
                  <th>Qué pasó</th>
                </tr>
              </thead>
              <tbody>
                {datos.truncated!.map((d) => (
                  <tr key={`${d.sellerId}:${d.date}`}>
                    <td className="pv-codigo">{d.date}</td>
                    <td>{d.seller}</td>
                    <td className="pv-codigo">{d.file}</td>
                    <td>{d.points}</td>
                    <td className="sub">{d.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
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
            Llegaron a Drive, pero el .gpx no se pudo leer. Estos fallaron con el
            lector viejo, que era todo o nada; el de ahora rescata lo que traiga un
            fichero cortado hasta donde llegue. <b>Volver a intentarlo</b> olvida el
            fichero para que n8n lo traiga otra vez y se lea con el lector de hoy —
            no borra nada de Drive. Si vuelve a fallar, es que está roto de verdad y
            hay que subirlo otra vez desde el teléfono.
          </p>
          {rotos.map((f) => (
            <FilaRota key={f.id} fichero={f} alReintentar={cargar} puede={puede("rutas.bandeja")} />
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
          <div className="tabla-ancha">
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
          </div>
        )}
      </div>

      {/*
        Los GPS: las carpetas de Drive.

        Esto estaba detrás de `carpetas.length > 0`, y dentro va TAMBIÉN el botón de dar
        de alta una carpeta. O sea: sólo se podían añadir carpetas cuando ya había
        carpetas. Con la lista vacía —o si `/api/gps` fallaba, que se tragaba el error sin
        decir nada— se perdían la tabla y el botón a la vez, y no quedaba forma de crear
        la primera.
      */}
      {puede("rutas.alias") && (
        <div className="tarjeta">
          <b>
            {carpetas.length > 0
              ? `Los GPS · ${sinDueno.length} de ${carpetas.length} sin asignar`
              : "Los GPS"}
          </b>
          <p className="sub">
            Cada carpeta compartida de Drive <b>es</b> un teléfono: se llama como el
            perfil del GPS y dentro están sus <span className="pv-codigo">AAAAMMDD.gpx</span>.
            Están todas aquí — no hay que teclear ninguna. Al decir de quién es una,
            se colocan de golpe <b>todos los ficheros suyos que estaban esperando</b> y
            los que entren a partir de ahora caen solos.
          </p>

          {sinAccesos && (
            <p className="aviso">
              No se pudo traer la gente de Accesos, así que el desplegable está vacío y
              no se puede asignar nada: {sinAccesos}
            </p>
          )}

          {sinCarpetas && (
            <p className="aviso">
              No se pudieron traer las carpetas: {sinCarpetas}. Lo de abajo está vacío por
              eso, no porque no haya ninguna.
            </p>
          )}

          {!sinCarpetas && carpetas.length === 0 && (
            <p className="sub">
              Todavía no hay ninguna carpeta. Se dan de alta solas la primera vez que n8n
              empuja un fichero de ellas; con el botón de abajo se puede dejar una
              preparada antes —con un teléfono recién entregado, así sus primeros días
              entran ya colocados—.
            </p>
          )}

          {carpetas.length > 0 && (
          <div className="tabla-ancha">
            <table className="movements tabla-gps">
              <thead>
                <tr>
                  <th>Carpeta (el GPS)</th>
                  {/* La columna sólo hace falta cuando NO se agrupa: si hay franja de
                      sucursal, la columna repite en cada fila lo que ya dice la franja. */}
                  {!agruparCarpetas && <th>Sucursal</th>}
                  <th>Rutas que trajo</th>
                  <th>Última</th>
                  <th>De quién es</th>
                </tr>
              </thead>
              <tbody>
                {carpetasPorSucursal.flatMap((grupo) => {
                  const sinColocar = grupo.filas.filter((g) => g.sellerId === "").length;

                  return (agruparCarpetas ? [(
                    <tr className="fila-sucursal" key={`s:${grupo.nombre}`}>
                      <th colSpan={4}>
                        {grupo.nombre}
                        <span className="fila-sucursal-cuenta">{grupo.filas.length} GPS</span>
                        {sinColocar > 0 && (
                          <span className="fila-sucursal-alerta">{sinColocar} sin asignar</span>
                        )}
                      </th>
                    </tr>
                  )] : []).concat(grupo.filas.map((g) => (
                  <tr key={g.id} data-sinduenno={g.sellerId === ""}>
                    <td>
                      {g.name}
                      {g.lastError && <span className="aviso">{g.lastError}</span>}
                    </td>
                    {!agruparCarpetas && <td className="pv-codigo">{g.branch || "—"}</td>}
                    <td>{g.files}</td>
                    <td className="pv-codigo">
                      {g.lastFile || "nunca"}
                      {g.daysSilent > 3 && (
                        <span className="seller-alerta">{g.daysSilent} días</span>
                      )}
                    </td>
                    <td>
                      {/*
                        También cuando YA tiene dueño: un teléfono cambia de manos, y sin
                        poder cambiarlo la carpeta se queda para siempre a nombre de quien
                        la llevaba antes — y con ella todo lo que suba a partir de hoy.
                      */}
                      <ElegirDueno
                        carpeta={g}
                        personas={personas}
                        alAsignar={cargar}
                        puedeAsignar={puede("rutas.alias")}
                      />
                    </td>
                  </tr>
                  )));
                })}
              </tbody>
            </table>
          </div>
          )}

          {/* Sin la llave no se ofrece: el botón llamaba a un endpoint que iba a
              contestar 403, y un botón que siempre falla es peor que no tenerlo. */}
          {puede("rutas.carpeta") && <NuevaCarpeta alCrear={cargar} />}
        </div>
      )}

      {/* Quién es quién con PEDIDO. UNA lista con todos. */}
      {puede("rutas.alias") && (datos.vendors?.length ?? 0) > 0 && (
        <div className="tarjeta">
          <b>
            Vendedores de PEDIDO · {sinDuenno.length} de {datos.vendors!.length} sin
            emparejar
          </b>
          <p className="sub">
            Sus nombres nacen en el maestro de vendedores y aquí nacen del nombre de
            una carpeta de Drive, así que no coinciden. Lo que no tiene duda se
            empareja solo; lo que le vale a dos, no se empareja y se pregunta. Es una
            vez por vendedor. <b>Mientras uno siga sin emparejar, sus pedidos no se
            cruzan con ninguna ruta.</b>
          </p>

          <div className="tabla-ancha">
            <table className="movements tabla-vendedores">
              <thead>
                <tr>
                  <th>En PEDIDO</th>
                  <th>Código</th>
                  {!agruparVendedores && <th>Sucursal</th>}
                  <th>Pedidos</th>
                  <th>Es, aquí</th>
                </tr>
              </thead>
              <tbody>
                {vendedoresPorSucursal.flatMap((grupo) => {
                  const sinEmparejar = grupo.filas.filter((v) => v.sellerId === "").length;

                  return (agruparVendedores ? [(
                    <tr className="fila-sucursal" key={`s:${grupo.nombre}`}>
                      <th colSpan={4}>
                        {grupo.nombre}
                        <span className="fila-sucursal-cuenta">{grupo.filas.length} vendedores</span>
                        {sinEmparejar > 0 && (
                          <span className="fila-sucursal-alerta">{sinEmparejar} sin emparejar</span>
                        )}
                      </th>
                    </tr>
                  )] : []).concat(grupo.filas.map((v) => (
                  <tr key={v.ref} data-sinduenno={v.sellerId === ""}>
                    <td>{v.name}</td>
                    <td className="pv-codigo">{v.code}</td>
                    {!agruparVendedores && <td className="pv-codigo">{v.branch || "—"}</td>}
                    <td>{v.orders}</td>
                    <td>
                      {v.sellerId ? (
                        <>
                          {v.seller}{" "}
                          <span className="sub">
                            {/* De dónde salió: si lo dijo el parecido de nombres es
                                revisable; si lo dijo una persona, no se toca. */}
                            {v.origin === "manual" ? "(lo dijo una persona)" : "(por el nombre)"}
                          </span>
                        </>
                      ) : (
                        <ElegirVendedor vendedor={v} vendedores={vendedores} alEmparejar={cargar} />
                      )}
                    </td>
                  </tr>
                  )));
                })}
              </tbody>
            </table>
          </div>
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

function ElegirVendedor({
  vendedor,
  vendedores,
  alEmparejar,
}: {
  vendedor: Vendedor;
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
        vendorCode: vendedor.code,
        vendorName: vendedor.name,
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
    <div className="elegir">
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
        {guardando ? "…" : "Es este"}
      </button>
      {fallo && <span className="aviso">{fallo}</span>}
    </div>
  );
}


function FilaRota({
  fichero,
  alReintentar,
  puede,
}: {
  fichero: FicheroAtascado;
  alReintentar: () => void;
  puede: boolean;
}) {
  const [yendo, setYendo] = useState(false);
  const [fallo, setFallo] = useState<string | null>(null);
  const [listo, setListo] = useState(false);

  /**
   * Un fichero que ya NO está en Drive no se puede reintentar.
   *
   * Reintentar es olvidar nuestra nota de «este ya lo vi» para que el siguiente empuje de
   * n8n lo traiga otra vez. Si Drive contesta 404, no hay nada que traer: el botón
   * prometía algo que no puede pasar y quien lo pulsaba se quedaba esperando.
   *
   * Ahí lo único que se puede hacer es quitarlo de la lista, que es la misma operación
   * pero dicha por lo que hace de verdad.
   */
  const noEstaEnDrive = /\b404\b|not found|no encontrado/i.test(fichero.error ?? "");

  async function reintentar() {
    setYendo(true);
    setFallo(null);
    try {
      await enviar(`/api/inbox/${fichero.id}/retry`, {});
      setListo(true);
      alReintentar();
    } catch (e) {
      setFallo((e as Error).message);
    } finally {
      setYendo(false);
    }
  }

  return (
    <div className="fila-suelta">
      <div>
        <b>{fichero.name}</b>
        <span className="sub">
          carpeta {fichero.source}
          {fichero.folderPath ? ` / ${fichero.folderPath}` : ""}
          {fichero.seller ? ` · ${fichero.seller}` : ""}
          {fichero.date ? ` · del ${fichero.date}` : ""} · {fichero.points} puntos
        </span>
        {fichero.error && <span className="aviso">{fichero.error}</span>}
      </div>
      {puede && (
        <div className="controles">
          <button
            className="pv-boton"
            disabled={yendo || listo}
            onClick={reintentar}
            title={
              noEstaEnDrive
                ? "Drive ya no tiene este fichero, así que no hay nada que volver a leer: esto sólo lo quita de la lista."
                : "Olvida nuestra nota de «este ya lo vi» para que n8n lo traiga otra vez y se lea con el lector de hoy."
            }
          >
            {listo
              ? (noEstaEnDrive ? "Quitado" : "Esperando a n8n…")
              : yendo
                ? (noEstaEnDrive ? "Quitando…" : "Olvidando…")
                : (noEstaEnDrive ? "Quitarlo de la lista" : "Volver a intentarlo")}
          </button>
        </div>
      )}
      {noEstaEnDrive && (
        <p className="sub">
          Drive ya no tiene este fichero (404), así que reintentarlo no puede funcionar:
          lo que hay que hacer es volver a subirlo desde el teléfono.
        </p>
      )}
      {fallo && <p className="aviso">{fallo}</p>}
    </div>
  );
}

function ElegirDueno({
  carpeta,
  personas,
  alAsignar,
  puedeAsignar,
}: {
  carpeta: Carpeta;
  personas: Persona[];
  alAsignar: () => void;
  puedeAsignar: boolean;
}) {
  const [quien, setQuien] = useState("");
  const [guardando, setGuardando] = useState(false);
  const [hecho, setHecho] = useState<string | null>(null);
  const [fallo, setFallo] = useState<string | null>(null);
  // Con dueño ya puesto, el desplegable no se enseña hasta que se pide cambiarlo: la
  // columna se lee de un vistazo y lo normal es no tocar nada.
  const [cambiando, setCambiando] = useState(false);

  async function asignar() {
    if (!quien) return;
    setGuardando(true);
    setFallo(null);
    try {
      const persona = personas.find((p) => p.authUserId === quien);
      const r = await enviar<{ placed: number; days: number }>(
        `/api/gps/${carpeta.id}/asignar`,
        { authUserId: quien, name: persona?.name ?? "" },
      );
      // Se dice CUÁNTO se arregló, que es la respuesta a «¿sirvió de algo?». Una
      // carpeta con doscientos días esperando se coloca entera de un clic.
      setHecho(
        r.placed > 0
          ? `${r.placed} ficheros colocados · ${r.days} días recalculados`
          : "asignada",
      );
      alAsignar();
    } catch (e) {
      setFallo((e as Error).message);
    } finally {
      setGuardando(false);
    }
  }

  if (hecho) return <span className="sub">{hecho}</span>;

  // Ya tiene dueño y nadie ha pedido cambiarlo: se enseña quién es y ya.
  if (carpeta.sellerId && !cambiando) {
    return (
      <span className="elegir-puesto">
        {carpeta.seller}
        {puedeAsignar && (
          <button className="pv-boton pv-boton-menudo" onClick={() => setCambiando(true)}>
            Cambiar
          </button>
        )}
      </span>
    );
  }

  // Sin la llave no se ofrece el desplegable: asignar contestaría 403.
  if (!puedeAsignar) return <span className="sub">sin asignar</span>;

  // Y sin gente que elegir, se dice por qué en vez de enseñar una lista vacía.
  if (personas.length === 0) {
    return <span className="sub">no se pudo traer la gente de Accesos</span>;
  }

  return (
    <div className="elegir">
      <select value={quien} onChange={(e) => setQuien(e.target.value)}>
        <option value="">¿De quién es?</option>
        {personas.map((p) => (
          <option key={p.authUserId} value={p.authUserId}>
            {p.name || p.email}
            {p.roles.length > 0 ? ` · ${p.roles.join(", ").toLowerCase()}` : ""}
          </option>
        ))}
      </select>
      <button
        className="pv-boton pv-boton-primario"
        disabled={!quien || guardando}
        onClick={asignar}
      >
        {guardando ? "…" : "Asignar"}
      </button>
      {cambiando && (
        <button className="pv-boton pv-boton-menudo" onClick={() => setCambiando(false)}>
          Dejarlo como está
        </button>
      )}
      {fallo && <span className="aviso">{fallo}</span>}
    </div>
  );
}

/**
 * Dar de alta una carpeta nueva.
 *
 * Casi nunca hace falta: las carpetas se dan de alta solas la primera vez que n8n
 * empuja un fichero de ellas. Esto es para el caso en que se comparte una carpeta
 * nueva y se quiere dejar preparada ANTES de que suba nada — con un teléfono recién
 * entregado, así sus primeros días entran ya colocados.
 */
function NuevaCarpeta({ alCrear }: { alCrear: () => void }) {
  const [abierto, setAbierto] = useState(false);
  const [nombre, setNombre] = useState("");
  const [folderId, setFolderId] = useState("");
  const [guardando, setGuardando] = useState(false);
  const [fallo, setFallo] = useState<string | null>(null);

  async function crear() {
    if (!nombre || !folderId) return;
    setGuardando(true);
    setFallo(null);
    try {
      await enviar("/api/sources", {
        name: nombre,
        folderId,
        // De un solo vendedor: es lo que es una carpeta de GPS. A quién pertenece se
        // dice después, en la lista de arriba.
        type: "VENDEDOR",
      });
      setNombre("");
      setFolderId("");
      setAbierto(false);
      alCrear();
    } catch (e) {
      setFallo((e as Error).message);
    } finally {
      setGuardando(false);
    }
  }

  if (!abierto) {
    return (
      <button className="pv-boton" style={{ marginTop: "0.75rem" }} onClick={() => setAbierto(true)}>
        Añadir una carpeta nueva
      </button>
    );
  }

  return (
    <div className="fila-suelta" style={{ marginTop: "0.75rem" }}>
      <div>
        <b>Carpeta nueva</b>
        <span className="sub">
          Solo hace falta si quieres dejarla lista ANTES de que suba nada: en cuanto
          n8n empuje su primer fichero, la carpeta se da de alta sola. El
          identificador es el trozo final de su dirección en Drive.
        </span>
      </div>
      <div className="controles">
        <input
          className="pv-campo"
          placeholder="Nombre del perfil del GPS"
          value={nombre}
          onChange={(e) => setNombre(e.target.value)}
        />
        <input
          className="pv-campo"
          placeholder="Identificador de la carpeta en Drive"
          value={folderId}
          onChange={(e) => setFolderId(e.target.value)}
          style={{ minWidth: 280 }}
        />
        <button
          className="pv-boton pv-boton-primario"
          disabled={!nombre || !folderId || guardando}
          onClick={crear}
        >
          {guardando ? "Guardando…" : "Añadir"}
        </button>
        <button className="pv-boton" onClick={() => setAbierto(false)}>Cancelar</button>
      </div>
      {fallo && <p className="aviso">{fallo}</p>}
    </div>
  );
}
