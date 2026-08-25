"use client";

/**
 * The viewer's map, por capas.
 *
 * Tres cosas distintas se dibujan sobre el mismo mapa y responden a preguntas
 * distintas, así que se encienden y se apagan por separado:
 *
 *   RECORRIDO  por dónde anduvo. La polilínea lleva un degradado en el tiempo
 *              —claro a oscuro— para que se vea el ORDEN sin reproducirlo: dónde
 *              empezó y hacia dónde fue. Una línea de un solo color solo dice por
 *              dónde pasó, y en una ruta que se cruza consigo misma eso no basta.
 *   PARADAS    dónde se detuvo, con el tamaño en proporción a lo que duró.
 *   CLIENTES   los del pedido de ese día: verde el que pisó, rojo el que no.
 *
 * Encenderlas y apagarlas es lo que permite la comparación que importa: apagando el
 * recorrido quedan los clientes solos —la ruta que TENÍA que hacer—, y encendiéndolo
 * encima se ve la que hizo.
 *
 * Las capas se crean UNA vez y se rellenan al cambiar los datos; encender o apagar
 * una no vuelve a dibujar el mapa entero.
 *
 * Leaflet touches `window`, so this component is loaded without SSR from the page.
 */

import { useEffect, useRef } from "react";
// La hoja de Leaflet, sin la cual los tiles se colocan sueltos por la caja: se
// perdió al reescribir este componente y el mapa salía a cuadros con huecos negros.
import "leaflet/dist/leaflet.css";
import type { Stop, TrackPoint, Visit } from "@/lib/api";

interface Props {
  points: TrackPoint[];
  stops: Stop[];
  visits: Visit[];
  /** Index up to which it is drawn, for the timeline. -1 = everything. */
  to?: number;
  /** Stop or client to centre on and open. Set from the list beside the map. */
  focusStopId?: string | null;
  focusClientId?: string | null;
}

// The gradient's ends, so the legend and the line cannot drift apart.
const INICIO = "hsl(210, 75%, 58%)";
const FIN = "hsl(20, 75%, 40%)";
// Verde y rojo del cliente, aquí una sola vez para que la leyenda no se desvíe.
const VISITADO = "#2f855a";
const SIN_VISITAR = "#c53030";

export default function RouteMap({
  points,
  stops,
  visits,
  to = -1,
  focusStopId,
  focusClientId,
}: Props) {
  const contenedor = useRef<HTMLDivElement>(null);
  const mapa = useRef<any>(null);
  const capaRuta = useRef<any>(null);
  const capaParadas = useRef<any>(null);
  const capaClientes = useRef<any>(null);
  const control = useRef<any>(null);
  // Las paradas y los clientes por id, para poder centrarlos desde la lista de al
  // lado.
  const marcas = useRef<Map<string, any>>(new Map());

  useEffect(() => {
    let cancelado = false;

    (async () => {
      const L = (await import("leaflet")).default;
      if (cancelado || !contenedor.current) return;

      if (!mapa.current) {
        mapa.current = L.map(contenedor.current, { zoomControl: true });
        L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
          attribution: "© OpenStreetMap",
          maxZoom: 19,
        }).addTo(mapa.current);
        // Las tres capas, encendidas de entrada, y el CONTROL DE CAPAS de Leaflet
        // —el mismo cuadradito apilado que se usa en OpenStreetMap—. Antes esto eran
        // tres casillas metidas en la barra de arriba, robándole sitio a los botones
        // de navegar y sin ninguna relación visual con el mapa al que mandan. El
        // control va DENTRO del mapa, que es donde se busca.
        capaRuta.current = L.layerGroup().addTo(mapa.current);
        capaParadas.current = L.layerGroup().addTo(mapa.current);
        capaClientes.current = L.layerGroup().addTo(mapa.current);

        control.current = L.control
          .layers(
            undefined,
            {
              Recorrido: capaRuta.current,
              Paradas: capaParadas.current,
              "Clientes del día": capaClientes.current,
            },
            { collapsed: true, position: "topright" },
          )
          .addTo(mapa.current);
      }

      capaRuta.current.clearLayers();
      capaParadas.current.clearLayers();
      capaClientes.current.clearLayers();
      marcas.current.clear();

      const visibles = to >= 0 ? points.slice(0, to + 1) : points;

      // Gradient by legs: each segment is painted according to its place in the day.
      for (let i = 1; i < visibles.length; i++) {
        const t = i / Math.max(1, visibles.length - 1);
        L.polyline(
          [
            [visibles[i - 1].lat, visibles[i - 1].lon],
            [visibles[i].lat, visibles[i].lon],
          ],
          {
            color: `hsl(${210 - t * 190}, 75%, ${58 - t * 18}%)`,
            weight: 4,
            opacity: 0.9,
          },
        ).addTo(capaRuta.current);
      }

      // Start and end of the workday.
      if (visibles.length > 0) {
        const primero = visibles[0];
        const ultimo = visibles[visibles.length - 1];
        L.circleMarker([primero.lat, primero.lon], {
          radius: 8,
          color: "#1f6feb",
          fillColor: "#1f6feb",
          fillOpacity: 1,
        })
          .bindPopup(`Empezó el día — ${hora(primero.ts)}`)
          .addTo(capaRuta.current);
        L.circleMarker([ultimo.lat, ultimo.lon], {
          radius: 8,
          color: "#8a1f1f",
          fillColor: "#d64545",
          fillOpacity: 1,
        })
          .bindPopup(`Terminó el día — ${hora(ultimo.ts)}`)
          .addTo(capaRuta.current);
      }

      // The stops, sized in proportion to how long they lasted: a half-hour visit
      // has to stand out against a traffic light.
      for (const p of stops) {
        const m = L.circleMarker([p.lat, p.lon], {
          radius: Math.min(28, 8 + p.durationMin / 3),
          color: "#e8833a",
          fillColor: "#f6b26b",
          fillOpacity: 0.55,
          weight: 2,
        })
          .bindPopup(
            `<b>Parada de ${p.durationMin} min</b><br>${hora(p.start)} – ${hora(
              p.end,
            )}${
              p.clientName
                ? `<br>a ${Math.round(p.clientDistM ?? 0)} m de ${p.clientName}`
                : ""
            }`,
          )
          .addTo(capaParadas.current);
        marcas.current.set(p.id, m);
      }

      // Los clientes del pedido de ese día. El color ES el veredicto: verde el que
      // pisó, rojo el que no. Y la distancia va en el globo aunque no cuente como
      // visita — «pasó a 180 m» no es lo mismo que «no se acercó en todo el día».
      for (const v of visits) {
        const m = L.circleMarker([v.lat, v.lon], {
          radius: 7,
          color: v.visited ? VISITADO : SIN_VISITAR,
          fillColor: v.visited ? VISITADO : SIN_VISITAR,
          fillOpacity: v.visited ? 0.9 : 0.35,
          weight: 2,
        })
          .bindPopup(
            `<b>${v.clientName}</b>` +
              (v.folio ? `<br>Pedido ${v.folio}` : "") +
              (v.address ? `<br>${v.address}` : "") +
              (v.visited
                ? `<br>Visitado a las ${hora(v.time)}${
                    v.minutes ? ` · ${v.minutes} min` : ""
                  }`
                : `<br>Sin visitar${
                    v.distanceM != null
                      ? ` · lo más cerca que pasó: ${Math.round(v.distanceM)} m`
                      : ""
                  }`),
          )
          .addTo(capaClientes.current);
        marcas.current.set(v.clientId, m);
      }

      // invalidateSize ANTES de encuadrar: Leaflet mide su caja al crearse, y aquí
      // se crea mientras la página todavía está colocando la rejilla. Si no se le
      // dice que vuelva a medir, calcula los tiles para un tamaño que ya no es el
      // suyo y el mapa sale a cuadros, con media caja en blanco.
      mapa.current.invalidateSize(false);

      // Se encuadra con TODO lo que hay —recorrido y clientes—, no solo con el
      // recorrido: un día sin fichero no tiene ni un punto, y encuadrar solo por
      // ellos dejaba a los clientes fuera de la pantalla y el mapa en Camagüey
      // aunque el vendedor fuera de Santiago.
      const todo: [number, number][] = [
        ...visibles.map((p) => [p.lat, p.lon] as [number, number]),
        ...visits.map((v) => [v.lat, v.lon] as [number, number]),
      ];
      if (todo.length === 0) {
        mapa.current.setView([21.38, -77.91], 12); // Camagüey
        return;
      }
      mapa.current.fitBounds(L.latLngBounds(todo), { padding: [30, 30] });
    })();

    return () => {
      cancelado = true;
    };
  }, [points, stops, visits, to]);

  // Y cada vez que la caja cambie de tamaño: al abrir el panel lateral, al girar
  // el móvil o al cambiar de zoom del navegador.
  useEffect(() => {
    if (!contenedor.current || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => mapa.current?.invalidateSize(false));
    ro.observe(contenedor.current);
    return () => ro.disconnect();
  }, []);

  // Tocar una parada o un cliente en la lista lo centra y lo abre en el mapa. Va en
  // su propio efecto para no volver a dibujarlo todo por mirar uno.
  useEffect(() => {
    const id = focusStopId || focusClientId;
    if (!id || !mapa.current) return;
    const m = marcas.current.get(id);
    if (!m) return;
    // Si su capa estaba apagada, tocar la fila de la lista no haría nada y parecería
    // que la pantalla está rota. Se enciende.
    const capa = focusClientId ? capaClientes.current : capaParadas.current;
    if (capa && !mapa.current.hasLayer(capa)) capa.addTo(mapa.current);
    mapa.current.setView(m.getLatLng(), Math.max(mapa.current.getZoom(), 16), {
      animate: true,
    });
    m.openPopup();
  }, [focusStopId, focusClientId]);

  return (
    <div className="mapa-caja">
      <div className="mapa" ref={contenedor} />

      {/* La leyenda, encima del mapa. Antes el dibujo afirmaba cosas que nadie
          podía leer: el grosor de un círculo naranja significa media hora parado,
          y sin decirlo es un adorno. Solo se explica lo que está encendido. */}
      <div className="leyenda-mapa">
        {(
          <div className="leyenda-fila">
            <span
              className="leyenda-linea"
              style={{ background: `linear-gradient(90deg, ${INICIO}, ${FIN})` }}
            />
            <span>Recorrido, del inicio al final del día</span>
          </div>
        )}
        {(
          <div className="leyenda-fila">
            <span className="leyenda-punto" style={{ background: "#1f6feb" }} />
            <span>Empezó</span>
            <span className="leyenda-punto" style={{ background: "#d64545" }} />
            <span>Terminó</span>
          </div>
        )}
        {(
          <div className="leyenda-fila">
            <span className="leyenda-parada leyenda-parada-chica" />
            <span className="leyenda-parada" />
            <span>Parada: el tamaño es lo que duró</span>
          </div>
        )}
        {visits.length > 0 && (
          <div className="leyenda-fila">
            <span className="leyenda-punto" style={{ background: VISITADO }} />
            <span>Cliente visitado</span>
            <span className="leyenda-punto" style={{ background: SIN_VISITAR }} />
            <span>Sin visitar</span>
          </div>
        )}
      </div>
    </div>
  );
}

function hora(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString("es", {
    hour: "2-digit",
    minute: "2-digit",
  });
}
