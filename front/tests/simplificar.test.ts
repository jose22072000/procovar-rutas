/**
 * El adelgazado del recorrido antes de dibujarlo.
 *
 * Lo que se comprueba no es «que devuelve menos puntos»: eso lo haría también tirar
 * uno de cada diez. Lo que importa es que la LÍNEA sea la misma, que las esquinas
 * sigan estando —son las que dicen por dónde giró— y que los índices que devuelve
 * sigan siendo los de la lista original, porque el deslizador de la línea de tiempo
 * cuenta sobre ella y un desfase aquí mueve el corte a otro sitio del mapa.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { indicesSimplificados, type Coordenada } from '../lib/simplificar.ts';

const M_POR_GRADO_LAT = 110574;

/** Un recorrido parecido al real: rectas, esquinas, curvas y el temblor del GPS. */
function rutaDeCiudad(n: number): Coordenada[] {
  const ps: Coordenada[] = [];
  let lat = 21.38;
  let lon = -77.91;
  let rumbo = 0;
  for (let i = 0; i < n; i++) {
    if (i % 400 === 0) rumbo += Math.PI / 2;
    rumbo += Math.sin(i / 900) * 0.004;
    lat += Math.cos(rumbo) * 0.000045;
    lon += Math.sin(rumbo) * 0.000045;
    ps.push({
      lat: lat + Math.sin(i * 1.7) * 0.000018,
      lon: lon + Math.cos(i * 2.3) * 0.000018,
    });
  }
  return ps;
}

function metrosAPolilinea(p: Coordenada, linea: Coordenada[]): number {
  const mLon = 111320 * Math.cos((p.lat * Math.PI) / 180);
  const px = p.lon * mLon;
  const py = p.lat * M_POR_GRADO_LAT;
  let mejor = Infinity;
  for (let i = 1; i < linea.length; i++) {
    const ax = linea[i - 1].lon * mLon;
    const ay = linea[i - 1].lat * M_POR_GRADO_LAT;
    const bx = linea[i].lon * mLon;
    const by = linea[i].lat * M_POR_GRADO_LAT;
    const dx = bx - ax;
    const dy = by - ay;
    const den = dx * dx + dy * dy;
    let t = 0;
    if (den > 0) t = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / den));
    const ex = px - (ax + t * dx);
    const ey = py - (ay + t * dy);
    const d = Math.hypot(ex, ey);
    if (d < mejor) mejor = d;
  }
  return mejor;
}

test('por debajo del tope no quita ni un punto', () => {
  const ps = rutaDeCiudad(40);
  assert.deepEqual(indicesSimplificados(ps, 2500), [...ps.keys()]);
});

test('una recta se queda en sus dos extremos', () => {
  const ps: Coordenada[] = [];
  for (let i = 0; i < 10000; i++) ps.push({ lat: 21.38 + i * 0.00001, lon: -77.91 });
  assert.deepEqual(indicesSimplificados(ps, 2500), [0, 9999]);
});

test('conserva los extremos, respeta el tope y devuelve los índices en orden', () => {
  const ps = rutaDeCiudad(80935); // el día completo de ANDY, 26/09/2026
  const idx = indicesSimplificados(ps, 2500);

  assert.equal(idx[0], 0, 'se perdió el primer punto del día');
  assert.equal(idx[idx.length - 1], ps.length - 1, 'se perdió el último punto del día');
  assert.ok(idx.length <= 2500, `pasó del tope: ${idx.length}`);
  assert.ok(idx.length > 100, `se pasó de frenada: ${idx.length}`);
  for (let i = 1; i < idx.length; i++) {
    assert.ok(idx[i] > idx[i - 1], `los índices no van en orden en la posición ${i}`);
  }
});

test('la línea simplificada es la MISMA línea', () => {
  const ps = rutaDeCiudad(30000);
  const linea = indicesSimplificados(ps, 2500).map((i) => ps[i]);

  let peor = 0;
  for (const p of ps) peor = Math.max(peor, metrosAPolilinea(p, linea));

  // Medido el 29/09/2026 sobre esta misma ruta: el desvío máximo es de 4 m, menos de
  // lo que tiembla el propio GPS. El listón en 8 m para que aflojar la tolerancia se
  // caiga aquí y no en el mapa de Jose.
  assert.ok(peor <= 8, `el recorrido simplificado se aparta ${peor.toFixed(1)} m del real`);
});

test('conserva las esquinas: el giro es la información', () => {
  const esquinas: Coordenada[] = [
    { lat: 21.38, lon: -77.91 },
    { lat: 21.38, lon: -77.89 },
    { lat: 21.4, lon: -77.89 },
    { lat: 21.4, lon: -77.91 },
    { lat: 21.38, lon: -77.91 },
  ];
  const ps: Coordenada[] = [];
  for (let i = 0; i < esquinas.length - 1; i++) {
    const a = esquinas[i];
    const b = esquinas[i + 1];
    for (let k = 0; k < 3000; k++) {
      const f = k / 3000;
      ps.push({ lat: a.lat + (b.lat - a.lat) * f, lon: a.lon + (b.lon - a.lon) * f });
    }
  }
  ps.push(esquinas[esquinas.length - 1]);

  const linea = indicesSimplificados(ps, 2500).map((i) => ps[i]);
  for (const e of esquinas) {
    const hay = linea.some(
      (p) => Math.abs(p.lat - e.lat) < 1e-5 && Math.abs(p.lon - e.lon) < 1e-5,
    );
    assert.ok(hay, `se perdió la esquina ${e.lat},${e.lon}: el mapa dibujaría un giro que no existe`);
  }
});
