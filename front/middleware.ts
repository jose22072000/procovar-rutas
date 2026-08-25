import { NextResponse, type NextRequest } from "next/server";

/**
 * Sin sesión no se pinta NADA: se va a Accesos antes de mandar HTML.
 *
 * Antes esto se decidía en el navegador: se servía la aplicación entera, el cliente
 * preguntaba `/api/me`, recibía un 401 y sólo entonces saltaba. Eso son segundos con
 * la barra y un "Cargando…" en pantalla, y antes de arreglarlo era peor todavía —
 * llegaba a pintar "No tienes permiso para ver el calendario", que manda a la gente a
 * pedir un permiso cuando lo que le falta es entrar.
 *
 * Aquí se puede resolver en el servidor porque el front y la API viven en el MISMO
 * dominio, así que la cookie que reparte Accesos (`qb.session_token`, compartida por
 * todo *.procovar.cloud) llega en la petición. Se mira si está y ya.
 *
 * Sólo se comprueba que EXISTA, no si vale: validarla es cosa de la API, que lo hace
 * en cada ruta. Aquí se resuelve el caso común —no hay cookie ninguna— sin pagar una
 * llamada de red. Una cookie caducada sigue el camino de siempre: entra, la API
 * contesta 401 y el cliente salta.
 */
const COOKIE = "qb.session_token";

export function middleware(req: NextRequest) {
  if (req.cookies.get(COOKIE)) return NextResponse.next();

  // La dirección PÚBLICA, no `req.url`.
  //
  // Detrás del proxy, `req.url` es la interna del contenedor —https://localhost:3601—
  // y mandarla como `returnTo` devuelve a la gente, después de entrar, a una dirección
  // que no existe. Se ve al probarlo desde fuera, nunca en local: en local coinciden.
  const host = req.headers.get("x-forwarded-host") ?? req.headers.get("host") ?? "";
  const proto = req.headers.get("x-forwarded-proto") ?? "https";
  const base = `${proto}://${host}`;
  const volverA = `${base}${req.nextUrl.pathname}${req.nextUrl.search}`;

  return NextResponse.redirect(
    `${base}/api/auth/login?returnTo=${encodeURIComponent(volverA)}`,
    307,
  );
}

export const config = {
  // Se deja fuera todo lo que no es una pantalla: la propia API (que es quien
  // contesta el login), los ficheros de Next, y los estáticos. Redirigir un .js
  // rompería la aplicación en vez de protegerla.
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico|logo.png|.*\\.(?:png|jpg|svg|ico|webmanifest)$).*)"],
};
