/**
 * El reparto del recorrido en tramos, y dónde lo corta el deslizador.
 *
 * Son dos cuentas de tres líneas, pero viven aquí y no dentro de RouteMap porque son
 * las que deciden QUÉ SE DIBUJA. Si el reparto deja un hueco, la línea sale partida;
 * si el corte cae un vértice más allá, el mapa enseña un trozo de recorrido que a esa
 * hora todavía no había pasado. Ninguna de las dos cosas da un error: dan un mapa
 * creíble y equivocado, que es lo caro. Dentro de un efecto de Leaflet no se pueden
 * probar; aquí sí.
 */

export interface Tramo {
  /** Primer vértice del tramo, dentro de la lista de vértices dibujados. */
  desde: number;
  /** Último vértice del tramo. Es el MISMO que el `desde` del siguiente. */
  hasta: number;
}

/**
 * Parte `nVertices` en como mucho `cuantos` tramos.
 *
 * Cada tramo COMPARTE su último vértice con el primero del siguiente. Si no, la línea
 * saldría con un hueco en cada costura: diez tramos, nueve agujeros.
 */
export function tramosDelRecorrido(nVertices: number, cuantos: number): Tramo[] {
  if (nVertices < 2 || cuantos < 1) return [];
  const n = Math.min(cuantos, nVertices - 1);
  const out: Tramo[] = [];
  for (let k = 0; k < n; k++) {
    out.push({
      desde: Math.round((k * (nVertices - 1)) / n),
      hasta: Math.round(((k + 1) * (nVertices - 1)) / n),
    });
  }
  return out;
}

/**
 * Hasta qué vértice se dibuja, dado el corte del deslizador.
 *
 * El deslizador cuenta sobre la lista ORIGINAL de puntos, y lo que se dibuja son los
 * vértices que sobrevivieron al adelgazado. `indices` dice qué posición original tenía
 * cada vértice; aquí se busca el último que no pasa del corte. Con `corte` negativo se
 * dibuja el día entero, que es como llega el visor al abrirse.
 */
export function ultimoVertice(indices: readonly number[], corte: number): number {
  if (indices.length === 0) return -1;
  if (corte < 0) return indices.length - 1;

  let lo = 0;
  let hi = indices.length - 1;
  while (lo < hi) {
    const med = Math.ceil((lo + hi) / 2);
    if (indices[med] <= corte) lo = med;
    else hi = med - 1;
  }
  return lo;
}
