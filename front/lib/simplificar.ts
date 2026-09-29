/**
 * Adelgazar un recorrido antes de dibujarlo (Douglas-Peucker).
 *
 * La API ya manda el recorrido adelgazado, así que esto es la segunda red: un
 * navegador con la página vieja en caché, una respuesta guardada por Cloudflare, o
 * cualquier día raro con más puntos de la cuenta. Si llegan pocos puntos no hace
 * nada y no cuesta nada.
 *
 * Devuelve los ÍNDICES de los puntos que se quedan, no los puntos: el deslizador de
 * la línea de tiempo cuenta sobre la lista original, y si aquí se devolviera otra
 * lista el corte del deslizador caería en otro sitio del mapa.
 */

export interface Coordenada {
  lat: number;
  lon: number;
}

/** Metros por grado, a la escala de un día de trabajo la tierra es plana de sobra. */
const M_POR_GRADO_LAT = 110574;

export function indicesSimplificados(
  puntos: readonly Coordenada[],
  maximo: number,
): number[] {
  const n = puntos.length;
  if (n <= maximo || maximo < 2) {
    return puntos.map((_, i) => i);
  }

  const mPorGradoLon = 111320 * Math.cos((puntos[0].lat * Math.PI) / 180);
  const xs = new Float64Array(n);
  const ys = new Float64Array(n);
  for (let i = 0; i < n; i++) {
    xs[i] = (puntos[i].lon - puntos[0].lon) * mPorGradoLon;
    ys[i] = (puntos[i].lat - puntos[0].lat) * M_POR_GRADO_LAT;
  }

  // Se empieza en un metro y se dobla hasta que cabe: una ruta llena de esquinas se
  // queda con una tolerancia fina, y una de carretera tolera más sin perder nada que
  // se vea.
  for (let tol = 1; tol <= 4096; tol *= 2) {
    const guardar = douglasPeucker(xs, ys, tol);
    let cuantos = 0;
    for (let i = 0; i < n; i++) if (guardar[i]) cuantos++;
    if (cuantos <= maximo) {
      const out: number[] = [];
      for (let i = 0; i < n; i++) if (guardar[i]) out.push(i);
      return out;
    }
  }

  // Todavía sobran: se corta uno de cada k, que al menos dibuja.
  const paso = Math.ceil(n / maximo);
  const out: number[] = [];
  for (let i = 0; i < n; i += paso) out.push(i);
  if (out[out.length - 1] !== n - 1) out.push(n - 1);
  return out;
}

/** Con pila propia y no con recursión: un día completo son decenas de miles de puntos. */
function douglasPeucker(
  xs: Float64Array,
  ys: Float64Array,
  tol: number,
): Uint8Array {
  const n = xs.length;
  const guardar = new Uint8Array(n);
  if (n === 0) return guardar;
  guardar[0] = 1;
  guardar[n - 1] = 1;
  if (n < 3) return guardar;

  const pila: number[] = [0, n - 1];
  const tol2 = tol * tol;
  while (pila.length > 0) {
    const b = pila.pop() as number;
    const a = pila.pop() as number;
    if (b - a < 2) continue;

    const dx = xs[b] - xs[a];
    const dy = ys[b] - ys[a];
    const den = dx * dx + dy * dy;

    let peor = -1;
    let peorD = 0;
    for (let i = a + 1; i < b; i++) {
      let d: number;
      if (den === 0) {
        // a y b son el mismo sitio: la «recta» no existe y se mide al propio punto.
        const ex = xs[i] - xs[a];
        const ey = ys[i] - ys[a];
        d = ex * ex + ey * ey;
      } else {
        const num = dx * (ys[a] - ys[i]) - dy * (xs[a] - xs[i]);
        d = (num * num) / den;
      }
      if (d > peorD) {
        peor = i;
        peorD = d;
      }
    }

    if (peor >= 0 && peorD > tol2) {
      guardar[peor] = 1;
      pila.push(a, peor, peor, b);
    }
  }
  return guardar;
}
