/**
 * Client for the routes API.
 *
 * The front end does NOT talk to the database or to procovar-auth: only to this
 * API, and always with the session cookie. All the control over who sees what
 * lives on the server — there is deliberately not a single role filter here: a
 * filter in the browser is a suggestion, not a restriction.
 */

export const API =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:3600";

export type DayStatus =
  | "OK"
  | "SIN_FICHERO"
  | "SIN_FECHA"
  | "SIN_MOVIMIENTO"
  | "MOVIMIENTO_ESCASO"
  | "NO_LABORABLE";

export interface SellerDay {
  sellerId: string;
  seller: string;
  branchId: string;
  branch: string;
  date: string;
  status: DayStatus;
  netKm: number;
  coverage: number;
  firstFix: string | null;
  lastFix: string | null;
  flags: string[];
  spreadM: number | null;
  placeLabel: string | null;
}

export interface SummaryRow {
  sellerId: string;
  seller: string;
  daysNoFile: number;
  daysNoDate: number;
  daysNoMovement: number;
  daysOk: number;
  totalKm: number;
}

export interface CalendarResponse {
  from: string;
  to: string;
  days: SellerDay[];
  summary: SummaryRow[];
  workdays: string[];
}

export interface Seller {
  id: string;
  name: string;
  branchId: string;
  active: boolean;
}

export interface TrackPoint {
  ts: string | null;
  lat: number;
  lon: number;
  speed: number | null;
  quality: string;
  seq: number;
}

export interface Stop {
  id: string;
  start: string;
  end: string;
  durationMin: number;
  lat: number;
  lon: number;
  clientName: string | null;
  clientDistM: number | null;
  seq: number;
}

export interface DayDetail {
  id: string;
  seller: string;
  branch: string;
  date: string;
  status: DayStatus;
  netKm: number;
  coverage: number;
  minMovement: number;
  minStopped: number;
  gaps: number;
  firstFix: string | null;
  lastFix: string | null;
  spreadM: number | null;
  flags: string[];
  placeLabel: string | null;
}

export interface DayResponse {
  day: DayDetail;
  points: TrackPoint[];
  stops: Stop[];
  timezone: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export async function ask<T>(path: string): Promise<T> {
  // credentials: "include" is essential: the session travels in the cookie
  // procovar-auth sets, and without this the browser will not send it to another
  // origin.
  const res = await fetch(`${API}${path}`, { credentials: "include" });

  if (res.status === 401) {
    // Expired session: off to the login, and back to where they were.
    if (typeof window !== "undefined") {
      window.location.href = `${API}/api/auth/login?returnTo=${encodeURIComponent(
        window.location.href,
      )}`;
    }
    throw new ApiError(401, "sin sesión");
  }

  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(res.status, body.error ?? `error ${res.status}`);
  }
  return body as T;
}

export async function enviar<T>(ruta: string, datos: unknown): Promise<T> {
  const res = await fetch(`${API}${ruta}`, {
    method: "POST",
    credentials: "include",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(datos),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(res.status, body.error ?? `error ${res.status}`);
  }
  return body as T;
}

// --- how statuses are presented ---------------------------------------------

// Only the wording lives here. The colour is in the stylesheet, keyed off
// data-status: it is Procovar's shared palette, and a colour written in the code
// would drift from Accesos and PEDIDO the first time one of them changes.
export const STATUS_LABEL: Record<DayStatus, string> = {
  OK: "Trabajó",
  SIN_FICHERO: "Sin fichero",
  SIN_FECHA: "Sin fecha",
  SIN_MOVIMIENTO: "Sin moverse",
  MOVIMIENTO_ESCASO: "Poco movimiento",
  NO_LABORABLE: "No laborable",
};

export const FLAG_LABEL: Record<string, string> = {
  entrada_tardia: "Empezó tarde",
  salida_temprana: "Terminó temprano",
  hueco_largo: "Se cortó la señal un rato",
  poca_cobertura: "Estuvo poco tiempo con señal",
  sin_movimiento: "No se movió del sitio en todo el día",
  movimiento_escaso: "Se movió muy poco",
  sin_horas: "El fichero no trae horas: se ve por dónde anduvo, pero no cuándo",
  sin_datos_en_jornada: "No hay nada entre las 9:00 y las 16:00",
};

export function shortDate(iso: string): string {
  const [a, m, d] = iso.slice(0, 10).split("-");
  return `${d}/${m}`;
}

export function dayName(iso: string): string {
  const days = ["Dom", "Lun", "Mar", "Mié", "Jue", "Vie", "Sáb"];
  return days[new Date(`${iso.slice(0, 10)}T12:00:00Z`).getUTCDay()];
}

/** Monday to Friday of the week containing the date. The working week is Mon–Fri. */
export function workWeek(iso: string): string[] {
  const d = new Date(`${iso}T12:00:00Z`);
  const isoDay = d.getUTCDay() === 0 ? 7 : d.getUTCDay();
  const monday = new Date(d.getTime() - (isoDay - 1) * 86400000);
  return Array.from({ length: 5 }, (_, i) =>
    new Date(monday.getTime() + i * 86400000).toISOString().slice(0, 10),
  );
}
