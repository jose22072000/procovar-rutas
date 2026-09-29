/**
 * El reparto en tramos y el corte del deslizador.
 *
 * Las dos cuentas que deciden qué línea se ve. Fallando, no dan un error: dan un mapa
 * creíble y equivocado —la línea partida, o un trozo de recorrido que a esa hora aún
 * no había pasado—, y eso no lo desmiente ninguna pantalla.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { tramosDelRecorrido, ultimoVertice } from '../lib/recorrido.ts';
import { indicesSimplificados, type Coordenada } from '../lib/simplificar.ts';

test('los tramos cubren el recorrido ENTERO y sin huecos', () => {
  for (const n of [2, 3, 7, 11, 100, 1999, 2000, 24924]) {
    const tramos = tramosDelRecorrido(n, 10);
    assert.equal(tramos[0].desde, 0, `n=${n}: el primer tramo no empieza en el principio`);
    assert.equal(
      tramos[tramos.length - 1].hasta,
      n - 1,
      `n=${n}: el último tramo no llega al final`,
    );
    for (let i = 1; i < tramos.length; i++) {
      assert.equal(
        tramos[i].desde,
        tramos[i - 1].hasta,
        `n=${n}: hueco entre el tramo ${i - 1} y el ${i}, la línea saldría partida`,
      );
    }
  }
});

test('ningún tramo va hacia atrás ni se queda vacío', () => {
  for (const n of [2, 5, 9, 10, 11, 37, 2000]) {
    for (const t of tramosDelRecorrido(n, 10)) {
      assert.ok(t.hasta > t.desde, `n=${n}: tramo vacío o invertido ${t.desde}-${t.hasta}`);
    }
  }
});

test('con menos vértices que tramos no se inventan tramos', () => {
  assert.equal(tramosDelRecorrido(3, 10).length, 2);
  assert.equal(tramosDelRecorrido(2, 10).length, 1);
  assert.deepEqual(tramosDelRecorrido(1, 10), []);
  assert.deepEqual(tramosDelRecorrido(0, 10), []);
});

test('el corte: «todo el día» dibuja hasta el último vértice', () => {
  const indices = [0, 5, 40, 90, 140];
  assert.equal(ultimoVertice(indices, -1), 4);
});

test('el corte cae en el último vértice que NO pasa del deslizador', () => {
  const indices = [0, 5, 40, 90, 140];
  assert.equal(ultimoVertice(indices, 0), 0);
  assert.equal(ultimoVertice(indices, 4), 0);
  assert.equal(ultimoVertice(indices, 5), 1);
  assert.equal(ultimoVertice(indices, 39), 1);
  assert.equal(ultimoVertice(indices, 40), 2);
  assert.equal(ultimoVertice(indices, 139), 3);
  assert.equal(ultimoVertice(indices, 140), 4);
  // Más allá del final —el deslizador va sobre la lista original, que es más larga—
  // se dibuja todo, no se sale de la lista.
  assert.equal(ultimoVertice(indices, 99999), 4);
});

test('el corte nunca va hacia atrás al mover el deslizador hacia delante', () => {
  const ps: Coordenada[] = [];
  let lat = 21.38;
  let lon = -77.91;
  for (let i = 0; i < 24924; i++) {
    lat += Math.cos(i / 300) * 0.00004;
    lon += Math.sin(i / 300) * 0.00004;
    ps.push({ lat, lon });
  }
  const indices = indicesSimplificados(ps, 2500);

  let anterior = -1;
  for (let corte = 0; corte < ps.length; corte += 7) {
    const v = ultimoVertice(indices, corte);
    assert.ok(v >= anterior, `el corte retrocedió en ${corte}`);
    assert.ok(indices[v] <= corte, `el corte ${corte} dibuja el punto ${indices[v]}, que aún no pasó`);
    if (v < indices.length - 1) {
      assert.ok(
        indices[v + 1] > corte,
        `el corte ${corte} se quedó corto: el vértice ${v + 1} también cabía`,
      );
    }
    anterior = v;
  }
});

test('sin vértices no se dibuja nada, y no se revienta', () => {
  assert.equal(ultimoVertice([], -1), -1);
  assert.equal(ultimoVertice([], 10), -1);
});
