/**
 * Cliente de la API de rutas.
 *
 * El frontend NO habla con la base de datos ni con procovar-auth: solo con esta
 * API, y siempre con la cookie de sesión. Todo el control de quién ve qué vive
 * en el servidor — aquí no hay ni un filtro por rol, a propósito: un filtro en
 * el navegador es una sugerencia, no una restricción.
 */

export const API =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:3600";

export type EstadoDia =
  | "OK"
  | "SIN_FICHERO"
  | "SIN_FECHA"
  | "SIN_MOVIMIENTO"
  | "MOVIMIENTO_ESCASO"
  | "NO_LABORABLE";

export interface DiaCalendario {
  TrabajadorID: string;
  Trabajador: string;
  SucursalID: string;
  Fecha: string;
  Estado: EstadoDia;
  KmNetos: number;
  Cobertura: number;
  PrimerFix: string | null;
  UltimoFix: string | null;
  Banderas: string[];
  RadioDispersion: number | null;
  LugarTexto: string | null;
}

export interface FilaResumen {
  TrabajadorID: string;
  Trabajador: string;
  SinFichero: number;
  SinFecha: number;
  SinMovimiento: number;
  DiasOk: number;
  KmTotal: number;
}

export interface RespuestaCalendario {
  desde: string;
  hasta: string;
  dias: DiaCalendario[];
  resumen: FilaResumen[];
  laborables: string[];
}

export interface PuntoRuta {
  Ts: string | null;
  Lat: number;
  Lon: number;
  Speed: number | null;
  Quality: string;
  Seq: number;
}

export interface Parada {
  ID: string;
  Inicio: string;
  Fin: string;
  DuracionMin: number;
  Lat: number;
  Lon: number;
  ClienteNombre: string | null;
  DistanciaClienteM: number | null;
  Seq: number;
}

export interface RespuestaDia {
  dia: {
    ID: string;
    Trabajador: string;
    Fecha: string;
    Estado: EstadoDia;
    KmNetos: number;
    Cobertura: number;
    MinMovimiento: number;
    MinParado: number;
    Huecos: number;
    PrimerFix: string | null;
    UltimoFix: string | null;
    RadioDispersion: number | null;
    Banderas: string[];
    LugarTexto: string | null;
  };
  puntos: PuntoRuta[];
  paradas: Parada[];
  zona: string;
}

export class ErrorApi extends Error {
  constructor(
    public estado: number,
    mensaje: string,
  ) {
    super(mensaje);
  }
}

export async function pedir<T>(ruta: string): Promise<T> {
  // credentials: "include" es imprescindible: la sesión va en la cookie que
  // pone procovar-auth, y sin esto el navegador no la manda a otro puerto.
  const res = await fetch(`${API}${ruta}`, { credentials: "include" });

  if (res.status === 401) {
    // Sesión caducada: al login, y que vuelva a donde estaba.
    if (typeof window !== "undefined") {
      window.location.href = `${API}/api/auth/entrar?volverA=${encodeURIComponent(
        window.location.href,
      )}`;
    }
    throw new ErrorApi(401, "sin sesión");
  }

  const cuerpo = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ErrorApi(res.status, cuerpo.error ?? `error ${res.status}`);
  }
  return cuerpo as T;
}

export async function enviar<T>(ruta: string, datos: unknown): Promise<T> {
  const res = await fetch(`${API}${ruta}`, {
    method: "POST",
    credentials: "include",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(datos),
  });
  const cuerpo = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ErrorApi(res.status, cuerpo.error ?? `error ${res.status}`);
  }
  return cuerpo as T;
}

// --- presentación de los estados -------------------------------------------

export const ESTADOS: Record<
  EstadoDia,
  { etiqueta: string; color: string; texto: string }
> = {
  OK: { etiqueta: "Trabajó", color: "var(--ok)", texto: "#0b3d1f" },
  SIN_FICHERO: { etiqueta: "Sin fichero", color: "var(--falta)", texto: "#fff" },
  SIN_FECHA: { etiqueta: "Sin fecha", color: "var(--aviso)", texto: "#3d2c00" },
  SIN_MOVIMIENTO: { etiqueta: "Sin moverse", color: "var(--alerta)", texto: "#fff" },
  MOVIMIENTO_ESCASO: { etiqueta: "Poco movimiento", color: "var(--flojo)", texto: "#3d2c00" },
  NO_LABORABLE: { etiqueta: "No laborable", color: "var(--gris)", texto: "#555" },
};

export const BANDERAS: Record<string, string> = {
  entrada_tardia: "Entró tarde",
  salida_temprana: "Salió temprano",
  hueco_largo: "Hueco de señal",
  poca_cobertura: "Poca cobertura",
  sin_movimiento: "No se movió del sitio",
  movimiento_escaso: "Movimiento escaso",
  sin_horas: "El fichero no trae horas",
  sin_datos_en_jornada: "Sin datos en la jornada",
};

export function fechaCorta(iso: string): string {
  const [a, m, d] = iso.slice(0, 10).split("-");
  return `${d}/${m}`;
}

export function nombreDia(iso: string): string {
  const dias = ["Dom", "Lun", "Mar", "Mié", "Jue", "Vie", "Sáb"];
  return dias[new Date(`${iso.slice(0, 10)}T12:00:00Z`).getUTCDay()];
}

/** Lunes a viernes de la semana que contiene la fecha. La jornada es L–V. */
export function semanaLaboral(iso: string): string[] {
  const d = new Date(`${iso}T12:00:00Z`);
  const isoDia = d.getUTCDay() === 0 ? 7 : d.getUTCDay();
  const lunes = new Date(d.getTime() - (isoDia - 1) * 86400000);
  return Array.from({ length: 5 }, (_, i) =>
    new Date(lunes.getTime() + i * 86400000).toISOString().slice(0, 10),
  );
}
