"use client";

/**
 * The panel's live notifications (SSE).
 *
 * The inbox and the queue change on their own: n8n pushes files and the ingest
 * service processes them while somebody is looking at the screen. Without this the
 * only way to find out was to reload, and the problem is not the nuisance — it is
 * that decisions get made on stale data: assigning a file somebody else has just
 * assigned, or staring at an empty queue that actually has twenty waiting.
 *
 * The data itself is not sent, only a "this changed". Whoever is listening asks
 * again for what they need. That way a lost notification does not leave the screen
 * lying, it only delays the refresh until the next one.
 */

import { useEffect, useRef } from "react";
import { API } from "./api";

export type EventType = "queue" | "file" | "scan" | "day";

/**
 * Calls `onChange` when one of the requested types arrives.
 *
 * If the server has no Redis, /api/events answers 503: the browser would retry in
 * a loop, so in that case it stops listening and the screen stays as it was, with
 * its reload button.
 */
export function useEvents(types: EventType[], onChange: () => void) {
  // The callback is kept in a ref so the connection is not reopened on every
  // render: otherwise each refresh would close and reopen the stream.
  const cb = useRef(onChange);
  cb.current = onChange;

  const key = types.join(",");

  useEffect(() => {
    const source = new EventSource(`${API}/api/events`, { withCredentials: true });

    const onArrive = () => cb.current();
    for (const t of key.split(",")) source.addEventListener(t, onArrive);

    source.onerror = () => {
      // readyState CLOSED means it will not reconnect on its own (503, or an
      // expired session). Close it for good rather than leave it trying.
      if (source.readyState === EventSource.CLOSED) source.close();
    };

    return () => source.close();
  }, [key]);
}
