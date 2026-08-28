/**
 * El agrupado por sucursal: el mismo criterio en las tres tablas.
 *
 * Se prueba aquí y no en cada pantalla porque lo que tiene que ser igual es ESTO: si el
 * calendario ordena de una forma y la pantalla de emparejar de otra, o una dice «Sin
 * sucursal» y la otra «—», parecen dos cosas distintas y nadie sabe cuál mirar.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { agruparPorSucursal, haceFaltaAgrupar, SIN_SUCURSAL } from '../lib/porSucursal.ts';

const filas = [
  { nombre: 'Zulema', sucursal: 'La Habana' },
  { nombre: 'Ana', sucursal: 'Camagüey' },
  { nombre: 'Bruno', sucursal: 'La Habana' },
  { nombre: 'Carla', sucursal: '' },
];

test('reparte las filas por sucursal, en orden alfabético', () => {
  const g = agruparPorSucursal(filas, (f) => f.sucursal, (f) => f.nombre);

  assert.deepEqual(g.map((x) => x.nombre), ['Camagüey', 'La Habana', SIN_SUCURSAL]);
  assert.equal(g[1].filas.length, 2);
});

test('dentro de cada grupo, las filas van ordenadas', () => {
  const g = agruparPorSucursal(filas, (f) => f.sucursal, (f) => f.nombre);

  assert.deepEqual(g[1].filas.map((f) => f.nombre), ['Bruno', 'Zulema']);
});

test('«Sin sucursal» va al FINAL, no al principio', () => {
  // Es la excepción; en lo alto de la pantalla desplazaría a las sucursales de verdad.
  const g = agruparPorSucursal(filas, (f) => f.sucursal);

  assert.equal(g[g.length - 1].nombre, SIN_SUCURSAL);
});

test('vacío, espacios y nulo son el MISMO grupo', () => {
  // Si no, un espacio de más parte «Sin sucursal» en dos grupos que se ven idénticos.
  const g = agruparPorSucursal(
    [{ s: '' }, { s: '   ' }, { s: null }, { s: undefined }],
    (f) => f.s as string | null | undefined,
  );

  assert.equal(g.length, 1);
  assert.equal(g[0].filas.length, 4);
});

test('con una sola sucursal NO se agrupa', () => {
  // Quien lleva una sucursal vería su nombre repetido veinte veces sin aprender nada.
  const una = agruparPorSucursal([{ s: 'La Habana' }, { s: 'La Habana' }], (f) => f.s);

  assert.equal(haceFaltaAgrupar(una), false);
  assert.equal(haceFaltaAgrupar(agruparPorSucursal(filas, (f) => f.sucursal)), true);
});

test('sin filas no hay grupos, y por tanto no se agrupa', () => {
  const g = agruparPorSucursal([], () => '');

  assert.equal(g.length, 0);
  assert.equal(haceFaltaAgrupar(g), false);
});
