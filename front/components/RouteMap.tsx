"use client";

/**
 * The viewer's map.
 *
 * The polyline carries a time gradient — light to dark — so the ORDER of the track
 * can be seen without replaying it: where it started and which way it went. A
 * single-colour line only says where someone passed, not in what order, and with a
 * route that crosses itself that is exactly what you need to know.
 *
 * It carries a legend because the drawing was making claims nobody could read: a
 * fat orange circle means "stopped here for half an hour" and a thin one means "a
 * traffic light", and without saying so out loud that is just decoration.
 *
 * Leaflet touches `window`, so this component is loaded without SSR from the page.
 */

import { useEffect, useRef } from "react";
import type { Stop, TrackPoint } from "@/lib/api";

interface Props {
  points: TrackPoint[];
  stops: Stop[];
  /** Index up to which it is drawn, for the timeline. -1 = everything. */
  to?: number;
  /** Stop to centre on and open. Set from the list beside the map. */
  focusStopId?: string | null;
}

// The gradient's ends, so the legend and the line cannot drift apart.
const INICIO = "hsl(210, 75%, 58%)";
const FIN = "hsl(20, 75%, 40%)";

export default function RouteMap({ points, stops, to = -1, focusStopId }: Props) {
  const contenedor = useRef<HTMLDivElement>(null);
  const mapa = useRef<any>(null);
  const capa = useRef<any>(null);
  // Las paradas por id, para poder centrarlas desde la lista de al lado.
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
        capa.current = L.layerGroup().addTo(mapa.current);
      }

      capa.current.clearLayers();
      marcas.current.clear();

      const visibles = to >= 0 ? points.slice(0, to + 1) : points;
      if (visibles.length === 0) {
        mapa.current.setView([21.38, -77.91], 12); // Camagüey
        return;
      }

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
        ).addTo(capa.current);
      }

      // Start and end of the workday.
      const primero = visibles[0];
      const ultimo = visibles[visibles.length - 1];
      L.circleMarker([primero.lat, primero.lon], {
        radius: 8,
        color: "#1f6feb",
        fillColor: "#1f6feb",
        fillOpacity: 1,
      })
        .bindPopup(`Empezó el día — ${hora(primero.ts)}`)
        .addTo(capa.current);
      L.circleMarker([ultimo.lat, ultimo.lon], {
        radius: 8,
        color: "#8a1f1f",
        fillColor: "#d64545",
        fillOpacity: 1,
      })
        .bindPopup(`Terminó el día — ${hora(ultimo.ts)}`)
        .addTo(capa.current);

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
          .addTo(capa.current);
        marcas.current.set(p.id, m);
      }

      // invalidateSize ANTES de encuadrar: Leaflet mide su caja al crearse, y aquí
      // se crea mientras la página todavía está colocando la rejilla. Si no se le
      // dice que vuelva a medir, calcula los tiles para un tamaño que ya no es el
      // suyo y el mapa sale a cuadros, con media caja en blanco.
      mapa.current.invalidateSize(false);

      const limites = L.latLngBounds(
        visibles.map((p) => [p.lat, p.lon] as [number, number]),
      );
      mapa.current.fitBounds(limites, { padding: [30, 30] });
    })();

    return () => {
      cancelado = true;
    };
  }, [points, stops, to]);

  // Y cada vez que la caja cambie de tamaño: al abrir el panel lateral, al girar
  // el móvil o al cambiar de zoom del navegador.
  useEffect(() => {
    if (!contenedor.current || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => mapa.current?.invalidateSize(false));
    ro.observe(contenedor.current);
    return () => ro.disconnect();
  }, []);

  // Tocar una parada en la lista la centra y la abre en el mapa. Va en su propio
  // efecto para no volver a dibujarlo todo por mirar una parada.
  useEffect(() => {
    if (!focusStopId || !mapa.current) return;
    const m = marcas.current.get(focusStopId);
    if (!m) return;
    mapa.current.setView(m.getLatLng(), Math.max(mapa.current.getZoom(), 16), {
      animate: true,
    });
    m.openPopup();
  }, [focusStopId]);

  return (
    <div className="mapa-caja">
      <div className="mapa" ref={contenedor} />

      {/* La leyenda, encima del mapa. Antes el dibujo afirmaba cosas que nadie
          podía leer: el grosor de un círculo naranja significa media hora parado,
          y sin decirlo es un adorno. */}
      <div className="leyenda-mapa">
        <div className="leyenda-fila">
          <span
            className="leyenda-linea"
            style={{ background: `linear-gradient(90deg, ${INICIO}, ${FIN})` }}
          />
          <span>Recorrido, del inicio al final del día</span>
        </div>
        <div className="leyenda-fila">
          <span className="leyenda-punto" style={{ background: "#1f6feb" }} />
          <span>Empezó</span>
          <span className="leyenda-punto" style={{ background: "#d64545" }} />
          <span>Terminó</span>
        </div>
        <div className="leyenda-fila">
          <span className="leyenda-parada leyenda-parada-chica" />
          <span className="leyenda-parada" />
          <span>Parada: el tamaño es lo que duró</span>
        </div>
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
