"use client";

/**
 * The compliance calendar: the landing screen, and now the only one.
 *
 * A grid of sellers × working days where each cell carries the colour of its
 * status. At a glance you see who is not working; touch a cell and you land on
 * that day's map.
 *
 * # Qué cambió, y por qué
 *
 * Antes la celda decía «Sin fichero» y ahí se acababa. Lo que hay que saber al ver
 * un hueco —¿no subió, o subió y se atascó?, ¿es de hoy o lleva un mes?— vivía en
 * la pantalla de Administración, y la de asignar un fichero suelto en la Bandeja.
 * Dos pantallas más a las que nadie entraba, así que el hueco se quedaba sin
 * explicar.
 *
 * Ahora todo eso está AQUÍ, encima de la cuadrícula, y solo cuando hay algo que
 * hacer: los que llevan días sin subir, los ficheros que llegaron sin dueño, y los
 * vendedores de PEDIDO que nadie ha emparejado. Un aviso que no aparece es un
 * problema que no existe.
 *
 * The colour lives in the stylesheet, keyed off data-status, and never in inline
 * styles: the palette is Procovar's, shared with Accesos and PEDIDO, and a colour
 * written here would drift from the others the first time one of them changes.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import RangoFechas from "@/components/RangoFechas";
import SinPermiso from "@/components/SinPermiso";
import { useSesion } from "@/components/Sesion";
import { useEvents } from "@/lib/events";
import {
  STATUS_LABEL,
  shortDate,
  dayName,
  ask,
  enviar,
  workWeek,
  diasEntre,
  porQueNoHayRuta,
  type DayStatus,
  type CalendarResponse,
  type SellerDay,
  type StuckDay,
  type SummaryRow,
  type Seller,
} from "@/lib/api";

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

// A partir de aquí se considera que el GPS de ese vendedor está fallando. Tres días
// cubre el fin de semana: la jornada es de lunes a viernes, así que un lunes por la
// mañana lo último es del viernes y eso es normal.
const DIAS_MALO = 3;

/** Un fichero que llegó y no se pudo colocar. */
interface FicheroSuelto {
  id: string;
  name: string;
  source: string;
  folderPath: string | null;
  status: string;
  error: string | null;
  aliasHint: string | null;
  seller: string;
  date: string | null;
  points: number;
}

/** Un vendedor de PEDIDO que no está emparejado con nadie de aquí. */
interface VendedorSuelto {
  branchId: string;
  branch: string;
  vendorCode: string;
  vendorName: string;
  orders: number;
  lastOrder: string;
}

export default function Calendar() {
  const router = useRouter();
  const { cargando, vetado, puede } = useSesion();
  // Un solo calendario, y sobre él se marca desde dónde hasta dónde. Antes eran un
  // campo de fecha y una lista de tramos ("su semana", "15 días desde ahí"): para
  // ver del 3 al 11 había que elegir el tramo que menos se pasara.
  //
  // Se entra viendo la semana laboral de hoy, que es lo que se quiere ver al entrar.
  const semana = useMemo(() => workWeek(todayISO()), []);
  const [desde, setDesde] = useState(semana[0]);
  const [hasta, setHasta] = useState(semana[4]);

  const [data, setData] = useState<CalendarResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Lo que hay que resolver, si esta persona puede resolverlo. Se pide aparte del
  // calendario y su fallo no lo tumba: son avisos sobre la cuadrícula, no la
  // cuadrícula.
  const [sueltos, setSueltos] = useState<FicheroSuelto[]>([]);
  const [sinEmparejar, setSinEmparejar] = useState<VendedorSuelto[]>([]);
  const [vendedores, setVendedores] = useState<Seller[]>([]);
  const [trayendo, setTrayendo] = useState(false);
  const [aviso, setAviso] = useState<string | null>(null);

  // Los días del rango, uno por columna. Se calculan aquí y no en el API porque es
  // la cuadrícula la que necesita saber qué columnas pintar, incluso las vacías.
  const week = useMemo(() => diasEntre(desde, hasta), [desde, hasta]);

  const cargarCalendario = useCallback(() => {
    setLoading(true);
    setError(null);
    ask<CalendarResponse>(`/api/calendar?from=${desde}&to=${hasta}`)
      .then(setData)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [desde, hasta]);

  useEffect(cargarCalendario, [cargarCalendario]);

  const cargarPendientes = useCallback(() => {
    if (puede("rutas.bandeja")) {
      ask<FicheroSuelto[]>("/api/inbox")
        .then((f) => setSueltos(f ?? []))
        .catch(() => setSueltos([]));
      ask<Seller[]>("/api/sellers")
        .then((v) => setVendedores(v ?? []))
        .catch(() => setVendedores([]));
    }
    if (puede("rutas.calendario")) {
      ask<{ unlinked: VendedorSuelto[] }>("/api/pedidos/vendedores")
        .then((r) => setSinEmparejar(r.unlinked ?? []))
        .catch(() => setSinEmparejar([]));
    }
  }, [puede]);

  useEffect(cargarPendientes, [cargarPendientes]);

  // Un fichero nuevo, un cruce nuevo: que la pantalla se entere sola.
  useEvents(["file", "day", "pedidos"], () => {
    cargarCalendario();
    cargarPendientes();
  });

  // The grid is assembled on the client: the API returns loose rows, and seller ×
  // day are crossed here so that cells go missing rather than whole rows.
  const bySeller = useMemo(() => {
    const map = new Map<
      string,
      { name: string; branch: string; days: Map<string, SellerDay> }
    >();
    for (const d of data?.days ?? []) {
      if (!map.has(d.sellerId)) {
        map.set(d.sellerId, { name: d.seller, branch: d.branch, days: new Map() });
      }
      map.get(d.sellerId)!.days.set(d.date.slice(0, 10), d);
    }
    return map;
  }, [data]);

  // Los ficheros atascados, indexados por celda: es la que explica el hueco.
  const atascos = useMemo(() => {
    const m = new Map<string, StuckDay>();
    for (const s of data?.stuck ?? []) m.set(`${s.sellerId}:${s.date}`, s);
    return m;
  }, [data]);

  const resumenDe = useCallback(
    (id: string): SummaryRow | undefined => data?.summary.find((r) => r.sellerId === id),
    [data],
  );

  // Correr el rango entero tantos días, manteniendo su ancho. Es lo que se quiere
  // al mirar "la semana pasada" sin volver a marcar dos días.
  function mover(days: number) {
    const corre = (f: string) =>
      new Date(new Date(`${f}T12:00:00Z`).getTime() + days * 86400000)
        .toISOString()
        .slice(0, 10);
    setDesde(corre(desde));
    setHasta(corre(hasta));
  }

  function estaSemana() {
    const s = workWeek(todayISO());
    setDesde(s[0]);
    setHasta(s[4]);
  }

  async function traerPedidos() {
    setTrayendo(true);
    setAviso(null);
    try {
      const r = await enviar<{ pedidos: number; cruces: number; emparejados: number }>(
        "/api/pedidos/sync",
        {},
      );
      setAviso(
        `${r.pedidos} pedidos traídos, ${r.cruces} días cruzados con su recorrido.`,
      );
      cargarCalendario();
      cargarPendientes();
    } catch (e) {
      setAviso((e as Error).message);
    } finally {
      setTrayendo(false);
    }
  }

  // Antes de pintar nada: si esto no es suyo, no se pinta. Ni la tabla a medias ni
  // el aviso encima de los datos.
  if (cargando) return <p className="cargando">Cargando…</p>;
  if (vetado) return <SinPermiso que="Rutas" detalle={vetado.replace("sin permiso: ", "")} />;
  if (!puede("rutas.calendario")) return <SinPermiso que="el calendario" detalle="rutas.calendario" />;

  const conPedidos = data?.withOrders ?? false;
  const callados = (data?.summary ?? []).filter(
    (r) => r.daysSilent === -1 || r.daysSilent > DIAS_MALO,
  );

  return (
    <>
      <h1>Calendario de cumplimiento</h1>
      <p className="sub">
        Cada celda es un día: tócala para ver el recorrido en el mapa.
        {conPedidos
          ? " Debajo de los kilómetros, a cuántos de sus clientes del día se acercó."
          : ""}
      </p>

      <div className="controles">
        <button className="pv-boton" onClick={() => mover(-7)} aria-label="Semana anterior">
          <svg className="flecha" viewBox="0 0 24 24" fill="none" aria-hidden>
            <path d="M14.5 6l-6 6 6 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
        <RangoFechas
          desde={desde}
          hasta={hasta}
          onCambio={(d, h) => {
            setDesde(d);
            setHasta(h);
          }}
        />
        <button className="pv-boton" onClick={() => mover(7)} aria-label="Semana siguiente">
          <svg className="flecha" viewBox="0 0 24 24" fill="none" aria-hidden>
            <path d="M9.5 6l6 6-6 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
        <button className="pv-boton" onClick={estaSemana}>Esta semana</button>
        {conPedidos && puede("rutas.barrido") && (
          <button className="pv-boton" disabled={trayendo} onClick={traerPedidos}>
            {trayendo ? "Trayendo…" : "Traer pedidos"}
          </button>
        )}
        <span className="controles-cuenta">
          {week.length} {week.length === 1 ? "día" : "días"}
          {bySeller.size > 0 ? ` · ${bySeller.size} vendedores` : ""}
        </span>
      </div>

      {error && <p className="aviso">{error}</p>}
      {aviso && <p className="sub">{aviso}</p>}
      {loading && <p className="loading">Cargando…</p>}

      {/* Los avisos, y SOLO cuando hay algo que hacer. Una tarjeta permanente que
          dice "no hay nada pendiente" es una línea que se aprende a saltar. */}
      <Avisos
        callados={callados}
        sueltos={sueltos}
        sinEmparejar={sinEmparejar}
        vendedores={vendedores}
        puedeAsignar={puede("rutas.bandeja")}
        puedeEmparejar={puede("rutas.alias")}
        alResolver={() => {
          cargarCalendario();
          cargarPendientes();
        }}
      />

      {!loading && bySeller.size === 0 && !error && (
        <div className="tarjeta">
          <p>
            No hay datos en ese rango. Si es la primera vez, las carpetas de Drive
            se dan de alta solas en cuanto la ingesta de n8n empuja el primer
            fichero de cada una.
          </p>
        </div>
      )}

      {bySeller.size > 0 && (
        <div className="tarjeta">
          {/* Dos maneras de enseñar lo mismo, y cada una donde funciona.
              En un monitor, la cuadrícula: vendedores por días, que es como se
              compara de un vistazo quién falló y quién no.
              En un teléfono, una ficha por vendedor con sus días en fila. Meter
              la cuadrícula en 380 píxeles obliga a arrastrar de lado para leer un
              renglón, y así no se compara nada: se pierde el nombre al mirar el
              jueves. */}
          <table className="rejilla solo-ancho">
            <thead>
              <tr>
                <th className="seller">Vendedor</th>
                {week.map((f) => (
                  <th key={f}>
                    {dayName(f)} {shortDate(f)}
                  </th>
                ))}
                <th>Subió</th>
                {conPedidos && <th>Clientes</th>}
                <th>Kilómetros</th>
              </tr>
            </thead>
            <tbody>
              {[...bySeller.entries()].map(([id, v]) => {
                const summary = resumenDe(id);
                const conFichero = week.filter((f) => {
                  const d = v.days.get(f);
                  return d && d.status !== "SIN_FICHERO";
                }).length;

                return (
                  <tr key={id}>
                    <td className="seller">
                      {v.name}
                      {/* La sucursal, debajo del nombre: es lo que dice si la
                          ingesta colocó a cada quien donde tocaba, y lo que el
                          gerente necesita reconocer de un vistazo. */}
                      <span className="seller-sucursal">{v.branch || "sin sucursal"}</span>
                      {/* Y si lleva días callado, se dice aquí y no en otra
                          pantalla: es la explicación de la fila entera. */}
                      {summary && (summary.daysSilent === -1 || summary.daysSilent > DIAS_MALO) && (
                        <span className="seller-alerta">
                          {summary.daysSilent === -1
                            ? "nunca ha subido"
                            : `${summary.daysSilent} días sin subir`}
                        </span>
                      )}
                      {conPedidos && summary && !summary.linked && (
                        <span className="seller-alerta">sin vendedor de PEDIDO</span>
                      )}
                    </td>

                    {week.map((f) => (
                      <td key={f}>
                        <Celda
                          dia={v.days.get(f)}
                          fecha={f}
                          vendedor={v.name}
                          atasco={atascos.get(`${id}:${f}`)}
                          resumen={summary}
                          conPedidos={conPedidos}
                          onAbrir={() => router.push(`/dia?seller=${id}&fecha=${f}`)}
                        />
                      </td>
                    ))}

                    <td>
                      <span
                        className="pastilla"
                        data-bien={conFichero === week.length}
                      >
                        {conFichero} de {week.length}
                      </span>
                    </td>

                    {conPedidos && (
                      <td>
                        {summary && summary.orders > 0 ? (
                          <span
                            className="pastilla"
                            data-bien={summary.visited === summary.orders}
                          >
                            {summary.visited} de {summary.orders}
                          </span>
                        ) : (
                          <span className="tenue">sin pedidos</span>
                        )}
                      </td>
                    )}

                    <td className="tenue">{(summary?.totalKm ?? 0).toFixed(0)} km</td>
                  </tr>
                );
              })}
            </tbody>
          </table>

          <div className="fichas-dias solo-estrecho">
            {[...bySeller.entries()].map(([id, v]) => {
              const summary = resumenDe(id);
              const conFichero = week.filter((f) => {
                const d = v.days.get(f);
                return d && d.status !== "SIN_FICHERO";
              }).length;

              return (
                <div className="ficha-dia" key={id}>
                  <div className="ficha-dia-cabecera">
                    <div>
                      <b>{v.name}</b>
                      <span className="seller-sucursal">{v.branch || "sin sucursal"}</span>
                      {summary && (summary.daysSilent === -1 || summary.daysSilent > DIAS_MALO) && (
                        <span className="seller-alerta">
                          {summary.daysSilent === -1
                            ? "nunca ha subido"
                            : `${summary.daysSilent} días sin subir`}
                        </span>
                      )}
                    </div>
                    <span className="pastilla" data-bien={conFichero === week.length}>
                      subió {conFichero}/{week.length}
                    </span>
                  </div>

                  <div className="ficha-dia-tira">
                    {week.map((f) => (
                      <Celda
                        key={f}
                        tira
                        dia={v.days.get(f)}
                        fecha={f}
                        vendedor={v.name}
                        atasco={atascos.get(`${id}:${f}`)}
                        resumen={summary}
                        conPedidos={conPedidos}
                        onAbrir={() => router.push(`/dia?seller=${id}&fecha=${f}`)}
                      />
                    ))}
                  </div>

                  <div className="ficha-dia-pie">
                    {(summary?.totalKm ?? 0).toFixed(0)} km en el rango
                    {conPedidos && summary && summary.orders > 0
                      ? ` · ${summary.visited} de ${summary.orders} clientes`
                      : ""}
                  </div>
                </div>
              );
            })}
          </div>

          <div className="leyenda">
            {(
              [
                "OK",
                "SIN_FICHERO",
                "SIN_FECHA",
                "SIN_MOVIMIENTO",
                "MOVIMIENTO_ESCASO",
              ] as DayStatus[]
            ).map((e) => (
              // The legend reuses the very same cell so the colours cannot drift
              // from the grid's.
              <span key={e} className="celda pastilla" data-status={e}>
                {STATUS_LABEL[e]}
              </span>
            ))}
          </div>
        </div>
      )}
    </>
  );
}

/**
 * Una celda del calendario.
 *
 * Cuando el día fue bien enseña lo que se hizo —kilómetros, hora de empezar y, si
 * hay pedidos, a cuántos clientes se acercó—. Cuando no, enseña QUÉ pasó, y al
 * pasar por encima, por qué.
 */
function Celda({
  dia,
  fecha,
  vendedor,
  atasco,
  resumen,
  conPedidos,
  onAbrir,
  tira = false,
}: {
  dia: SellerDay | undefined;
  fecha: string;
  vendedor: string;
  atasco: StuckDay | undefined;
  resumen: SummaryRow | undefined;
  conPedidos: boolean;
  onAbrir: () => void;
  tira?: boolean;
}) {
  // No row for that day is painted as a miss: that is what it is, and this way an
  // absence is never invisible.
  const status: DayStatus = dia?.status ?? "SIN_FICHERO";
  const hubo = status === "OK" || status === "MOVIMIENTO_ESCASO";
  const porque = hubo ? "" : porQueNoHayRuta(status, atasco, resumen);

  return (
    <button
      className={tira ? "celda celda-tira" : "celda"}
      data-status={status}
      title={`${vendedor} · ${fecha} · ${porque || STATUS_LABEL[status]}`}
      onClick={onAbrir}
    >
      {tira && <span className="celda-tira-dia">{dayName(fecha)}</span>}

      {hubo ? (
        <>
          <span className="km">{dia?.netKm.toFixed(1)} km</span>
          {!tira && dia?.firstFix
            ? new Date(dia.firstFix).toLocaleTimeString("es", {
                hour: "2-digit",
                minute: "2-digit",
              })
            : null}
          {/* Los clientes del día, debajo de los kilómetros. Es lo que convierte
              «hizo 30 km» en «hizo la ruta»: treinta kilómetros se hacen igual
              dando vueltas. */}
          {conPedidos && dia?.orders != null && dia.orders > 0 && (
            <span className="celda-clientes" data-bien={dia.visited === dia.orders}>
              {dia.visited}/{dia.orders} clientes
            </span>
          )}
        </>
      ) : (
        <span className="celda-motivo">{porque}</span>
      )}
    </button>
  );
}

/**
 * Lo que hay que resolver, si hay algo que resolver.
 *
 * Cada bloque aparece SOLO si tiene contenido y si esta persona tiene la llave para
 * tocarlo. Un bloque vacío o un botón que va a contestar 403 no son información,
 * son ruido encima de la cuadrícula.
 */
function Avisos({
  callados,
  sueltos,
  sinEmparejar,
  vendedores,
  puedeAsignar,
  puedeEmparejar,
  alResolver,
}: {
  callados: SummaryRow[];
  sueltos: FicheroSuelto[];
  sinEmparejar: VendedorSuelto[];
  vendedores: Seller[];
  puedeAsignar: boolean;
  puedeEmparejar: boolean;
  alResolver: () => void;
}) {
  const hayFicheros = puedeAsignar && sueltos.length > 0;
  const hayVendedores = puedeEmparejar && sinEmparejar.length > 0;

  if (callados.length === 0 && !hayFicheros && !hayVendedores) return null;

  return (
    <div className="tarjeta avisos">
      {callados.length > 0 && (
        <div className="aviso-bloque">
          <b>
            {callados.length === 1
              ? "Un vendedor lleva días sin subir"
              : `${callados.length} vendedores llevan días sin subir`}
          </b>
          <p className="sub">
            O no llevan el GPS encendido, o dejó de subir. Más de {DIAS_MALO} días
            callado es un teléfono que hay que ir a mirar.
          </p>
          <div className="lista-callados">
            {callados
              .slice()
              .sort((a, b) => (b.daysSilent < 0 ? 1e9 : b.daysSilent) - (a.daysSilent < 0 ? 1e9 : a.daysSilent))
              .map((c) => (
                <span key={c.sellerId} className="pv-etiqueta pv-etiqueta-cuno">
                  {c.seller} ·{" "}
                  {c.daysSilent === -1 ? "nunca" : `${c.daysSilent} días`}
                </span>
              ))}
          </div>
        </div>
      )}

      {hayFicheros && (
        <div className="aviso-bloque">
          <b>
            {sueltos.length === 1
              ? "Un fichero llegó y no se pudo colocar"
              : `${sueltos.length} ficheros llegaron y no se pudieron colocar`}
          </b>
          <p className="sub">
            Al asignarlos se recuerda el dispositivo, y los siguientes de ese mismo
            teléfono se colocan solos. Es una vez por dispositivo, no todos los días.
          </p>
          {sueltos.slice(0, 10).map((f) => (
            <FilaSuelta key={f.id} fichero={f} vendedores={vendedores} alAsignar={alResolver} />
          ))}
          {sueltos.length > 10 && (
            <p className="sub">y {sueltos.length - 10} más.</p>
          )}
        </div>
      )}

      {hayVendedores && (
        <div className="aviso-bloque">
          <b>
            {sinEmparejar.length === 1
              ? "Un vendedor de PEDIDO no está emparejado"
              : `${sinEmparejar.length} vendedores de PEDIDO no están emparejados`}
          </b>
          <p className="sub">
            Sus pedidos no se cruzan con ninguna ruta mientras sigan así. El
            emparejamiento automático solo se atreve cuando no hay duda: si un
            nombre le vale a dos, prefiere no decir nada. Esto se hace una vez por
            vendedor.
          </p>
          {sinEmparejar.slice(0, 10).map((v) => (
            <FilaVendedor
              key={`${v.branchId}:${v.vendorCode}`}
              vendedor={v}
              vendedores={vendedores}
              alEmparejar={alResolver}
            />
          ))}
        </div>
      )}
    </div>
  );
}

const MOTIVOS: Record<string, string> = {
  SIN_ASIGNAR: "No se supo de quién es",
  SIN_FECHA: "No se pudo fechar",
  ERROR: "No se pudo leer",
};

function FilaSuelta({
  fichero,
  vendedores,
  alAsignar,
}: {
  fichero: FicheroSuelto;
  vendedores: Seller[];
  alAsignar: () => void;
}) {
  const [seller, setVendedor] = useState("");
  const [fecha, setFecha] = useState(fichero.date?.slice(0, 10) ?? "");
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
        // Recordar SIEMPRE: el alias es lo que hace que esto sea una vez por
        // dispositivo y no una vez por día. Era una casilla que había que marcar, y
        // desmarcarla no tenía ningún uso salvo condenarse a repetir el trabajo.
        rememberAlias: true,
        alias: fichero.aliasHint ?? "",
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
          {MOTIVOS[fichero.status] ?? fichero.status} · {fichero.source}
          {fichero.folderPath ? ` / ${fichero.folderPath}` : ""} · {fichero.points} puntos
          {fichero.aliasHint ? ` · el fichero dice «${fichero.aliasHint}»` : ""}
        </span>
        {fichero.error && <span className="aviso">{fichero.error}</span>}
      </div>

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
      )}
      {fallo && <p className="aviso">{fallo}</p>}
    </div>
  );
}

function FilaVendedor({
  vendedor,
  vendedores,
  alEmparejar,
}: {
  vendedor: VendedorSuelto;
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
          {vendedor.branch} · {vendedor.orders}{" "}
          {vendedor.orders === 1 ? "pedido" : "pedidos"} · el último el{" "}
          {vendedor.lastOrder}
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
