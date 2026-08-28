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

/**
 * Lo que necesita la pantalla de Revisar.
 *
 * Con carpetas y SIN carpetas: el caso de la lista vacía es el que tenía el fallo —el
 * botón de crear vivía dentro del bloque de la tabla, así que no se podía crear la
 * primera—. Con `SIN_CARPETAS=1` se devuelve vacío para poder comprobarlo.
 */
const sinCarpetas = process.env.SIN_CARPETAS === '1';

const CARPETAS = sinCarpetas ? [] : [
  { id: 'g1', name: 'GPS luis', folderId: 'f1', branch: 'Bayamo', files: 12, lastFile: '2026-08-27', daysSilent: 1, sellerId: 's1', seller: 'Luis', linked: true, stuckFiles: 0, lastError: null },
  { id: 'g2', name: 'GPS ALFREDO', folderId: 'f2', branch: 'Camagüey', files: 4, lastFile: '2026-08-20', daysSilent: 8, sellerId: '', seller: '', linked: false, stuckFiles: 2, lastError: null },
  { id: 'g3', name: 'GPS Georli', folderId: 'f3', branch: 'Camagüey', files: 9, lastFile: '2026-08-26', daysSilent: 2, sellerId: '', seller: '', linked: false, stuckFiles: 0, lastError: null },
];

const PERSONAS = [
  { authUserId: 'p1', name: 'Luis Verdecia', email: 'luis@procovar.test', branch: 'Bayamo', roles: ['GESTOR'], sellerId: 's1' },
  { authUserId: 'p2', name: 'Alfredo Hernández', email: '', branch: 'Camagüey', roles: ['SUPERVISOR'], sellerId: '' },
];

/** Con `SIN_PERSONAS=1` la lista de gente falla: sirve para probar que se DICE. */
const sinPersonas = process.env.SIN_PERSONAS === '1';

const RESPUESTAS = {
  '/api/me': {
    user: 'Admin Global',
    email: 'admin@procovar.test',
    role: 'admin',
    branchId: '',
    isAdmin: true,
    permisos: {
      // Todas las de rutas: esta cuenta de mentira es la del desarrollador, y lo que se
      // está mirando es la pantalla, no quién puede verla.
      'rutas.entrar': true,
      'rutas.calendario': true,
      'rutas.alias': true,
      'rutas.visor': true,
      'rutas.reporte': true,
      'rutas.bandeja': true,
      'rutas.administracion': true,
      'rutas.carpeta': true,
      'rutas.barrido': true,
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
  '/api/gps': CARPETAS,
  '/api/personas': sinPersonas ? undefined : PERSONAS,
  '/api/review': {
    files: [
      // Uno roto que Drive YA NO TIENE: reintentarlo no puede funcionar nunca.
      { id: 'f-404', name: '20260818.gpx', source: 'MAYLEN', folderPath: 'MAYLEN', status: 'ERROR', error: 'XML ilegible: XML syntax error on line 9 · error 404', seller: 'MAYLEN', date: null, points: 0 },
      // Y uno que sí se puede volver a leer con el lector de hoy.
      { id: 'f-xml', name: '20260504.gpx', source: 'GEORLI', folderPath: 'GEORLI', status: 'ERROR', error: 'XML ilegible: EOF', seller: 'GEORLI', date: null, points: 0 },
    ],
    silent: [],
    vendors: [
      { ref: 'v1', name: 'ANDY ALMANZA', code: 'andy.almanza', branch: 'Camagüey', orders: 42, sellerId: '', seller: '', origin: '' },
      { ref: 'v2', name: 'LUIS VERDECIA', code: 'luis.verdecia', branch: 'Bayamo', orders: 18, sellerId: 's1', seller: 'Luis', origin: 'manual' },
    ],
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

  if (req.method === 'OPTIONS') {
    res.setHeader('Access-Control-Allow-Methods', 'GET,POST,DELETE,OPTIONS');
    res.setHeader('Access-Control-Allow-Headers', 'content-type');
    res.statusCode = 204;
    res.end();
    return;
  }

  /**
   * Lo que se ESCRIBE: dar de alta una carpeta y decir de quién es.
   *
   * Se guarda en la lista de arriba para que al recargar salga puesto, que es lo que hay
   * que comprobar: un «Asignar» que contesta bien pero no cambia nada se ve igual.
   */
  if (req.method === 'POST') {
    let texto = '';

    req.on('data', (c) => { texto += c; });
    req.on('end', () => {
      const datos = JSON.parse(texto || '{}');

      if (ruta === '/api/sources') {
        CARPETAS.push({
          id: `g${CARPETAS.length + 1}`, name: datos.name, folderId: datos.folderId,
          branch: 'Camagüey', files: 0, lastFile: '', daysSilent: 0,
          sellerId: '', seller: '', linked: false, stuckFiles: 0, lastError: null,
        });
        res.end(JSON.stringify({ ok: true }));
        return;
      }

      const asignar = ruta.match(/^\/api\/gps\/([^/]+)\/asignar$/);

      if (asignar) {
        const c = CARPETAS.find((x) => x.id === asignar[1]);

        if (c) { c.sellerId = datos.authUserId; c.seller = datos.name; c.linked = true; }
        res.end(JSON.stringify({ placed: 3, days: 2 }));
        return;
      }

      res.statusCode = 404;
      res.end(JSON.stringify({ error: `sin ruta ${ruta}` }));
    });
    return;
  }

  const cuerpo = RESPUESTAS[ruta];

  if (cuerpo === undefined) {
    res.statusCode = 404;
    res.end(JSON.stringify({ error: `sin ruta ${ruta}` }));
    return;
  }
  res.end(JSON.stringify(cuerpo));
}).listen(PUERTO, () => console.log(`api de mentira en http://localhost:${PUERTO}`));
