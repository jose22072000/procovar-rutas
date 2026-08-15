"use client";

/**
 * The viewer's map.
 *
 * The polyline carries a time gradient — light to dark — so the ORDER of the track
 * can be seen without replaying it: where it started and which way it went. A
 * single-colour line only says where someone passed, not in what order, and with a
 * route that crosses itself that is exactly what you need to know.
 *
 * Leaflet touches `window`, so this component is loaded without SSR from the
 * página.
 */

import { useEffect, useRef } from "react";
import type { Stop, TrackPoint } from "@/lib/api";
import "leaflet/dist/leaflet.css";

interface Props {
  points: TrackPoint[];
  stops: Stop[];
  /** Index up to which it is drawn, for the timeline. -1 = everything. */
  to?: number;
}

export default function MapaRuta({ points, stops, to = -1 }: Props) {
  const contenedor = useRef<HTMLDivElement>(null);
  const mapa = useRef<any>(null);
  const capa = useRef<any>(null);

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
        .bindPopup(`Primer fix — ${hora(primero.ts)}`)
        .addTo(capa.current);
      L.circleMarker([ultimo.lat, ultimo.lon], {
        radius: 8,
        color: "#8a1f1f",
        fillColor: "#d64545",
        fillOpacity: 1,
      })
        .bindPopup(`Último fix — ${hora(ultimo.ts)}`)
        .addTo(capa.current);

      // The stops, sized in proportion to how long they lasted: a half-hour visit
      // has to stand out against a traffic light.
      for (const p of stops) {
        L.circleMarker([p.lat, p.lon], {
          radius: Math.min(28, 8 + p.durationMin / 3),
          color: "#e8833a",
          fillColor: "#f6b26b",
          fillOpacity: 0.55,
          weight: 2,
        })
          .bindPopup(
            `<b>Stop de ${p.durationMin} min</b><br>${hora(p.start)} – ${hora(
              p.end,
            )}${
              p.clientName
                ? `<br>a ${Math.round(p.clientDistM ?? 0)} m de ${p.clientName}`
                : ""
            }`,
          )
          .addTo(capa.current);
      }

      const limites = L.latLngBounds(
        visibles.map((p) => [p.lat, p.lon] as [number, number]),
      );
      mapa.current.fitBounds(limites, { padding: [30, 30] });
    })();

    return () => {
      cancelado = true;
    };
  }, [points, stops, to]);

  return <div className="mapa" ref={contenedor} />;
}

function hora(iso: string | null): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString("es", {
    hour: "2-digit",
    minute: "2-digit",
  });
}
