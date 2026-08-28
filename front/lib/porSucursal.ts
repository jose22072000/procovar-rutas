/**
 * Agrupar por sucursal, con el mismo criterio en todas las pantallas.
 *
 * Aquí se listaban vendedores, carpetas de GPS y vendedores de PEDIDO en tablas planas,
 * con la sucursal escrita en una columna o en letra pequeña bajo el nombre. Con ocho
 * sucursales eso obliga a leer ochenta y dos filas comparando ese dato para encontrar las
 * tuyas — y quien mira esto pregunta «cómo va Camagüey», no «cómo va la fila 47».
 *
 * Está aquí y no copiado en cada pantalla porque las tres tienen que ordenar igual y
 * llamar igual a lo mismo: una que diga «Sin sucursal» y otra «—» para el mismo caso
 * parecen dos cosas distintas.
 */

/** Lo que se enseña cuando la fila no tiene sucursal. No es un hueco: es un problema. */
export const SIN_SUCURSAL = "Sin sucursal";

export interface Grupo<T> {
  nombre: string;
  filas: T[];
}

/**
 * Reparte las filas por sucursal, ordenadas por nombre.
 *
 * `sucursalDe` saca el nombre de la sucursal y `ordenarPor` el texto por el que se
 * ordenan las filas dentro de cada grupo. «Sin sucursal» va al final: es la excepción, y
 * en lo alto de la pantalla desplazaría a las ocho sucursales de verdad.
 */
export function agruparPorSucursal<T>(
  filas: T[],
  sucursalDe: (fila: T) => string | null | undefined,
  ordenarPor?: (fila: T) => string,
): Grupo<T>[] {
  const grupos = new Map<string, Grupo<T>>();

  for (const fila of filas) {
    const nombre = (sucursalDe(fila) || "").trim() || SIN_SUCURSAL;

    if (!grupos.has(nombre)) grupos.set(nombre, { nombre, filas: [] });
    grupos.get(nombre)!.filas.push(fila);
  }

  if (ordenarPor) {
    for (const g of grupos.values()) {
      g.filas.sort((a, b) => ordenarPor(a).localeCompare(ordenarPor(b)));
    }
  }

  return [...grupos.values()].sort((a, b) => {
    if (a.nombre === SIN_SUCURSAL) return 1;
    if (b.nombre === SIN_SUCURSAL) return -1;
    return a.nombre.localeCompare(b.nombre);
  });
}

/**
 * ¿Hace falta separar?
 *
 * Con una sola sucursal a la vista, un encabezado que la repita no dice nada que no se
 * sepa ya: quien lleva una sucursal vería su nombre veinte veces. Se separa sólo cuando
 * hay más de una.
 */
export const haceFaltaAgrupar = (grupos: Grupo<unknown>[]) => grupos.length > 1;
