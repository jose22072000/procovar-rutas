"use client";

/**
 * Los avisos en vivo del panel (SSE).
 *
 * La bandeja y la cola cambian solas: n8n empuja ficheros y el servicio de
 * ingesta los procesa mientras alguien mira la pantalla. Sin esto, la única forma
 * de enterarse era recargar, y el problema no es la molestia — es que se toman
 * decisiones sobre datos viejos: se asigna un fichero que otra persona acaba de
 * asignar, o se mira una cola vacía que en realidad tiene veinte esperando.
 *
 * No se transmiten los datos, solo un "esto cambió". Quien escucha vuelve a pedir
 * lo que necesita. Así un aviso perdido no deja la pantalla mintiendo, solo
 * retrasa la actualización hasta el siguiente.
 */

import { useEffect, useRef } from "react";
import { API } from "./api";

export type TipoEvento = "queue" | "file" | "scan" | "day";

/**
 * Llama a `alCambiar` cuando llegue alguno de los tipos pedidos.
 *
 * Si el servidor no tiene Redis, /api/events responde 503: el navegador
 * reintentaría en bucle, así que en ese caso se deja de escuchar y la pantalla se
 * queda como estaba, con su botón de recargar.
 */
export function useEventos(tipos: TipoEvento[], alCambiar: () => void) {
  // La función se guarda en una referencia para no reabrir la conexión en cada
  // render: si no, cada actualización cerraría y abriría el flujo.
  const cb = useRef(alCambiar);
  cb.current = alCambiar;

  const clave = tipos.join(",");

  useEffect(() => {
    const fuente = new EventSource(`${API}/api/events`, { withCredentials: true });

    const alLlegar = () => cb.current();
    for (const t of clave.split(",")) fuente.addEventListener(t, alLlegar);

    fuente.onerror = () => {
      // readyState CLOSED significa que no va a reconectar solo (503, o sesión
      // caducada). Se cierra del todo para no dejarlo intentándolo.
      if (fuente.readyState === EventSource.CLOSED) fuente.close();
    };

    return () => fuente.close();
  }, [clave]);
}
