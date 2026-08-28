/**
 * Un API de mentira para poder MIRAR la pantalla.
 *
 * La de verdad es un servicio en Go con Postgres y sesión de Accesos detrás. Levantar
 * todo eso para comprobar que una tabla se agrupa bien es desproporcionado, y sin
 * levantarlo no se comprueba nada: esta pantalla la pinta el navegador entero, así que el
 * HTML del servidor viene vacío.
 *
 * Devuelve lo mínimo que la pantalla pide, con vendedores de VARIAS sucursales — que es
 * justo el caso que había que separar.
 *
 * Uso:  node scripts/api-de-mentira.mjs [puerto]
 */

import { createServer } from 'node:http';

const PUERTO = Number(process.argv[2] || 3600);

const SUCURSALES = [
  { id: 'HAB', nombre: 'La Habana' },
  { id: 'CMG', nombre: 'Camagüey' },
  { id: 'HOL', nombre: 'Holguín' },
];

// Cinco días laborables terminando hoy.
const dias = [];
for (let i = 4; i >= 0; i--) {
  dias.push(new Date(Date.now() - i * 86400000).toISOString().slice(0, 10));
}

const ESTADOS = ['OK', 'SIN_FICHERO', 'SIN_MOVIMIENTO', 'OK', 'SIN_FECHA'];

const days = [];
const summary = [];

SUCURSALES.forEach((s, si) => {
  for (let v = 1; v <= 3; v++) {
    const sellerId = `${s.id}-${v}`;
    const seller = `Vendedor ${s.id}${v}`;

    dias.forEach((fecha, di) => {
      days.push({
        sellerId,
        seller,
        branchId: s.id,
        branch: s.nombre,
        date: `${fecha}T00:00:00Z`,
        status: ESTADOS[(si + v + di) % ESTADOS.length],
        netKm: 10 + di * 3,
        coverage: 0.9,
        firstFix: null,
        lastFix: null,
        flags: [],
        spreadM: null,
      });
    });

    summary.push({
      sellerId,
      seller,
      daysNoFile: 1,
      daysNoDate: 0,
      daysNoMovement: 0,
      daysOk: 4,
      totalKm: 40 + v * 7,
      lastUpload: dias[4],
      // Uno por sucursal lleva días callado: es lo que cuenta el encabezado del grupo.
      daysSilent: v === 2 ? 9 : 0,
      stuckFiles: 0,
      linked: true,
      orders: 12,
      visited: v === 3 ? 12 : 8,
    });
  }
});

const RESPUESTAS = {
  '/api/me': {
    user: 'Admin Global',
    email: 'admin@procovar.test',
    role: 'admin',
    branchId: '',
    isAdmin: true,
    permisos: {
      'rutas.calendario': true,
      'rutas.alias': true,
      'rutas.dia': true,
      'rutas.reporte': true,
      'rutas.revisar': true,
    },
  },
  '/api/calendar': {
    from: dias[0],
    to: dias[4],
    days,
    summary,
    stuck: [],
    withOrders: true,
    workdays: dias,
  },
  '/api/inbox': [],
  '/api/sellers': [],
  '/api/pedidos/vendedores': { unlinked: [] },
};

createServer((req, res) => {
  const ruta = req.url.split('?')[0];

  res.setHeader('Access-Control-Allow-Origin', req.headers.origin || '*');
  res.setHeader('Access-Control-Allow-Credentials', 'true');
  res.setHeader('Content-Type', 'application/json; charset=utf-8');

  const cuerpo = RESPUESTAS[ruta];

  if (cuerpo === undefined) {
    res.statusCode = 404;
    res.end(JSON.stringify({ error: `sin ruta ${ruta}` }));
    return;
  }
  res.end(JSON.stringify(cuerpo));
}).listen(PUERTO, () => console.log(`api de mentira en http://localhost:${PUERTO}`));
