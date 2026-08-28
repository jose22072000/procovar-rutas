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
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Icon } from "@iconify/react";
import { flechaIzquierda, flechaDerecha, traer, aviso as iconoAviso, ir } from "@/components/iconos";
import RangoFechas from "@/components/RangoFechas";
import SinPermiso from "@/components/SinPermiso";
import { useSesion } from "@/components/Sesion";
import { useEvents } from "@/lib/events";
import { agruparPorSucursal, haceFaltaAgrupar, leerPlegadas, guardarPlegadas } from "@/lib/porSucursal";
import {
  STATUS_LABEL,
  FLAG_LABEL,
  shortDate,
  dayName,
  ask,
  enviar,
  workWeek,
  diasEntre,
  porQueNoHayRuta,
  CORTADO,
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

  /**
   * Los vendedores AGRUPADOS POR SUCURSAL.
   *
   * Salían todos en una sola tabla con la sucursal escrita bajo cada nombre. Con ocho
   * sucursales eso es leer ochenta y dos filas comparando una línea pequeña para saber
   * cuáles son las tuyas — y quien mira esto lo que quiere es «cómo va Camagüey», no
   * «cómo va el vendedor número 47».
   *
   * Se agrupa sólo si hay más de una sucursal a la vista: si todos son de la misma, un
   * encabezado que lo repita no dice nada que no se sepa ya.
   */
  const porSucursal = useMemo(
    () => agruparPorSucursal([...bySeller.entries()], ([, v]) => v.branch, ([, v]) => v.name),
    [bySeller],
  );

  const agrupar = haceFaltaAgrupar(porSucursal);

  /**
   * Qué sucursales están cerradas.
   *
   * Se lee del navegador en un efecto y no al montar el estado: en el servidor no hay
   * localStorage, así que leerlo ahí daría un HTML distinto del que pinta el navegador y
   * React se queja de que no coinciden.
   */
  const [plegadas, setPlegadas] = useState<Set<string>>(new Set());

  useEffect(() => setPlegadas(leerPlegadas("calendario")), []);

  const plegar = (nombre: string) => {
    setPlegadas((antes) => {
      const ahora = new Set(antes);

      if (ahora.has(nombre)) ahora.delete(nombre);
      else ahora.add(nombre);
      guardarPlegadas("calendario", ahora);
      return ahora;
    });
  };

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
      // El botón ENCOLA, no descarga: son tres semanas de días y bajárselas de
      // golpe dejaría el botón pensando un minuto y le metería a PEDIDO —de quien
      // dependen las sucursales para trabajar— justo la ráfaga que la cola existe
      // para evitar. El trabajador los va haciendo de uno en uno, y la pantalla se
      // entera sola por los avisos en vivo.
      const r = await enviar<{ encolados: number; cola: { pendientes: number } | null }>(
        "/api/pedidos/sync",
        {},
      );
      setAviso(
        r.encolados === 0
          ? "Ya estaba todo encolado; el trabajador los va trayendo."
          : `${r.encolados} ${r.encolados === 1 ? "día" : "días"} encolados. ` +
            "Se van trayendo de uno en uno para no cargar a PEDIDO; la pantalla se actualiza sola.",
      );
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
  const pendientes = callados.length + sueltos.length + sinEmparejar.length;

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
          <Icon icon={flechaIzquierda} className="icono" aria-hidden />
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
          <Icon icon={flechaDerecha} className="icono" aria-hidden />
        </button>
        <button className="pv-boton" onClick={estaSemana}>Esta semana</button>
        {conPedidos && puede("rutas.barrido") && (
          <button className="pv-boton" disabled={trayendo} onClick={traerPedidos}>
            <Icon icon={traer} className="icono" aria-hidden />
            {trayendo ? "Encolando…" : "Traer pedidos"}
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

      {/* UNA LÍNEA, y solo cuando hay algo. El detalle —qué fichero falló y por qué,
          quién no sube y desde cuándo— vive en su propia pantalla: aquí dentro
          convertía la cuadrícula en un muro de texto que hay que atravesar para
          llegar a lo que se venía a ver, y encima el detalle salía a medias porque
          no cabe. */}
      {pendientes > 0 && (
        <Link href="/revisar" className="tarjeta linea-revisar">
          <Icon icon={iconoAviso} className="icono" aria-hidden />
          <b>
            {pendientes === 1
              ? "Hay 1 cosa que revisar"
              : `Hay ${pendientes} cosas que revisar`}
          </b>
          <span className="sub">
            {[
              callados.length > 0 &&
                `${callados.length} ${callados.length === 1 ? "vendedor lleva" : "vendedores llevan"} días sin subir`,
              sueltos.length > 0 &&
                `${sueltos.length} ${sueltos.length === 1 ? "fichero llegó" : "ficheros llegaron"} y no se pudieron usar`,
              sinEmparejar.length > 0 &&
                `${sinEmparejar.length} sin emparejar con PEDIDO`,
            ]
              .filter(Boolean)
              .join(" · ")}
          </span>
          <span className="linea-revisar-ir">
            Ver el detalle
            <Icon icon={ir} className="icono" aria-hidden />
          </span>
        </Link>
      )}

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
              {porSucursal.flatMap((grupo) => {
                /**
                 * Cuántos de esa sucursal llevan días callados.
                 *
                 * Es el número por el que se mira esta pantalla: no «cuántos vendedores
                 * tiene Camagüey» —eso no cambia— sino cuántos de ellos hay que perseguir
                 * hoy. Puesto en el encabezado se ve sin recorrer el grupo entero.
                 */
                const callados = grupo.filas.filter(([id]) => {
                  const r = resumenDe(id);
                  return r && (r.daysSilent === -1 || r.daysSilent > DIAS_MALO);
                }).length;

                const cerrada = plegadas.has(grupo.nombre);
                const cabecera = agrupar ? [(
                  <tr className="fila-sucursal" key={`s:${grupo.nombre}`}>
                    <th colSpan={week.length + (conPedidos ? 4 : 3)}>
                      {/* Toda la franja es el botón, no una flechita de doce píxeles: se
                          pulsa ochenta veces al día y hay que poder acertar sin mirar. */}
                      <button
                        type="button"
                        className="plegador"
                        aria-expanded={!cerrada}
                        onClick={() => plegar(grupo.nombre)}
                      >
                        <span className="plegador-flecha" data-cerrada={cerrada}>▾</span>
                        {grupo.nombre}
                        <span className="fila-sucursal-cuenta">{grupo.filas.length} vendedores</span>
                        {callados > 0 && (
                          <span className="fila-sucursal-alerta">{callados} sin subir</span>
                        )}
                      </button>
                    </th>
                  </tr>
                )] : [];

                // Cerrada: se pinta la franja y nada más. La cuenta y el aviso siguen
                // ahí, así que cerrar una sucursal no esconde que tiene gente sin subir.
                if (cerrada) return cabecera;

                return cabecera.concat(grupo.filas.map(([id, v]) => {
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
                          ingesta colocó a cada quien donde tocaba. Cuando la tabla va
                          agrupada, el encabezado ya lo dice y repetirlo en cada fila es
                          decir dos veces lo mismo. */}
                      {!agrupar && (
                        <span className="seller-sucursal">{v.branch || "sin sucursal"}</span>
                      )}
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
                }));
              })}
            </tbody>
          </table>

          <div className="fichas-dias solo-estrecho">
            {porSucursal.flatMap((grupo) => {
              const cerrada = plegadas.has(grupo.nombre);
              const cabecera = agrupar ? [(
              <button
                type="button"
                className="ficha-sucursal plegador"
                key={`s:${grupo.nombre}`}
                aria-expanded={!cerrada}
                onClick={() => plegar(grupo.nombre)}
              >
                <span>
                  <span className="plegador-flecha" data-cerrada={cerrada}>▾</span>
                  {grupo.nombre}
                </span>
                <span>{grupo.filas.length}</span>
              </button>
            )] : [];

              if (cerrada) return cabecera;

              return cabecera.concat(grupo.filas.map(([id, v]) => {
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
                      {!agrupar && (
                        <span className="seller-sucursal">{v.branch || "sin sucursal"}</span>
                      )}
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
            }));
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
  const cortado = dia?.flags?.includes(CORTADO) ?? false;

  return (
    <button
      className={tira ? "celda celda-tira" : "celda"}
      data-status={status}
      title={`${vendedor} · ${fecha} · ${
        porque || STATUS_LABEL[status]
      }${cortado ? ` · ${FLAG_LABEL[CORTADO]}` : ""}`}
      onClick={onAbrir}
    >
      {tira && <span className="celda-tira-dia">{dayName(fecha)}</span>}

      {hubo ? (
        <>
          <span className="km">
            {dia?.netKm.toFixed(1)} km
            {/* Si el fichero llegó cortado, estos kilómetros son los del trozo que
                se pudo leer. Sin decirlo aquí, medio día se lee como el día — y es
                justo en esta celda donde se lee. */}
            {cortado && <b className="celda-cortado" title={FLAG_LABEL[CORTADO]}>a medias</b>}
          </span>
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
