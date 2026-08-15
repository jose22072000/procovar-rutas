"use client";

/**
 * El mapa del visor.
 *
 * La polilínea va con degradado temporal —de claro a oscuro— para que se vea el
 * ORDEN del track sin tener que reproducirlo: dónde empezó y hacia dónde
 * fue. Una línea de un solo color solo dice por dónde pasó, no en qué orden, y
 * con una ruta que se cruza consigo misma eso es justo lo que hace falta saber.
 *
 * Leaflet toca `window`, así que este componente se carga sin SSR desde la
 * página.
 */

import { useEffect, useRef } from "react";
import type { Parada, PuntoRuta } from "@/lib/api";
import "leaflet/dist/leaflet.css";

interface Props {
  points: PuntoRuta[];
  stops: Parada[];
  /** Índice hasta el que se pinta, para la línea de tiempo. -1 = todo. */
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

      // Degradado por tramos: cada segmento se pinta según su posición en el día.
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

      // Inicio y fin de la workday.
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

      // Las stops, con el tamaño en proporción a lo que duraron: una visita de
      // media hora tiene que saltar a la vista frente a un semáforo.
      for (const p of stops) {
        L.circleMarker([p.lat, p.lon], {
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
