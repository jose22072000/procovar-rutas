#!/usr/bin/env python3
"""Qué carpetas tienen .gpx y no están dadas de alta en el panel.

No toca nada: sólo mira y cuenta.

Hace falta porque las carpetas no se pueden dar de alta a ojo por el nombre. En los
Drive conviven los perfiles de GPS con las carpetas de la ingesta de pedidos
(`PEDIDOS`, `PROCESADOS`, `ERRORES`) y con cosas sueltas (`Visitas`, `Copia`,
`Untitled folder`), y dar de alta una de esas crea un "vendedor" llamado así. Lo que
decide es si dentro hay `.gpx`, y eso hay que preguntárselo a Drive.

De cada cuenta se piden DOS cosas y con eso sale todo: sus carpetas, y todos sus
`.gpx` de una vez con el `parents` de cada uno. Agrupando por padre sale la cuenta
por carpeta sin una llamada por carpeta.

Uso:  python3 inventario-carpetas.py <clave-api-n8n>
"""
import json
import subprocess
import sys

N8N = "https://n8n.procovar.cloud/api/v1"
RUTAS = "https://rutas.procovar.cloud"
CLAVE = "14a657aaaf962ef033b1c508b6f6555f225a0c99d90f98358575d33e80f8cf98"
DRIVE = "https://www.googleapis.com/drive/v3/files"
CARPETA = "application/vnd.google-apps.folder"

PROVINCIAS = {
    "Camaguey": "PtjfVbn3s8kIoB1H",
    "Granma": "cQlTlorOaKNu2RSP",
    "Guantanamo": "g5TNh0qpREWnoX3j",
    "Habana": "Xh9UXcYLsDhZwANZ",
    "Holguin": "52A8c5qMws3HltVi",
    "LasTunas": "fHefRFz6HQfXt0Eh",
    "SantiSpiritus": "kXkGLZjFQ2Pu1Q2z",
    "Santiago": "sPEHylrec6dQWsEE",
}


def http(nombre, x, y, params, cred=None):
    p = {"options": {}}
    if cred:
        p["authentication"] = "predefinedCredentialType"
        p["nodeCredentialType"] = "googleDriveOAuth2Api"
    p.update(params)
    n = {"name": nombre, "type": "n8n-nodes-base.httpRequest", "typeVersion": 4.2,
         "position": [x, y], "parameters": p}
    if cred:
        n["credentials"] = {"googleDriveOAuth2Api": {"id": cred[0], "name": cred[1]}}
    return n


def consulta(*pares):
    return {"sendQuery": True,
            "queryParameters": {"parameters": [{"name": k, "value": v} for k, v in pares]}}


# Drive devuelve como mucho 1000 por página, y Camagüey y Santiago pasan de ahí. Sin
# esto el recuento sale corto justo en las provincias grandes, que son las que
# importan, y encima sin avisar. Cada página llega como un ítem y se juntan abajo.
PAGINAR = {"pagination": {"pagination": {
    "paginationMode": "updateAParameterInEachRequest",
    "parameters": {"parameters": [
        {"type": "qs", "name": "pageToken", "value": "={{ $response.body.nextPageToken }}"},
    ]},
    "paginationCompleteWhen": "other",
    "completeExpression": "={{ !$response.body.nextPageToken }}",
    "limitPagesFetched": True,
    "maxRequests": 25,
}}}


LAS_DE_RUTAS = """
// Las carpetas dadas de alta, en un solo ítem: sueltas, cada cuenta de abajo se
// dispararía una vez por carpeta.
return [{ json: { ids: $input.all().map(i => i.json.folderId) } }];
"""

CUENTA = """
// Cada carpeta de esta cuenta, con cuántos .gpx tiene y si el panel la conoce.
//
// Los dos nodos de arriba vienen paginados, así que cada página es un ítem y hay que
// juntarlas: con una sola página se contaba de menos justo en las provincias grandes,
// que son las que importan.
const alta = new Set($('Las de Rutas').first().json.ids);
const carpetas = $('Carpetas de %(prov)s').all().flatMap(i => i.json.files || []);
const gpx = $input.all().flatMap(i => i.json.files || []);

const cuantos = new Map();
for (const f of gpx) {
  for (const p of (f.parents || [])) cuantos.set(p, (cuantos.get(p) || 0) + 1);
}

// "GPS Procesados" es el archivo: ahí es donde la ingesta aparta lo que ya entró, así
// que está llena de .gpx por definición y no es candidata a nada. Darla de alta sería
// crear un vendedor llamado así y volver a ingerir lo ya ingerido.
const ARCHIVO = 'GPS Procesados';

const filas = carpetas
  .filter(c => c.name !== ARCHIVO)
  .map(c => ({
    provincia: '%(prov)s',
    carpeta: c.name,
    carpetaId: c.id,
    gpx: cuantos.get(c.id) || 0,
    // Lo que ya entró por ella y está apartado: una carpeta "vacía" que tiene su
    // archivo lleno no está muerta, está al día.
    apartados: (carpetas.find(h => h.name === ARCHIVO && (h.parents || []).includes(c.id))
      ? cuantos.get(carpetas.find(h => h.name === ARCHIVO && (h.parents || []).includes(c.id)).id) || 0
      : 0),
    dadaDeAlta: alta.has(c.id),
  }));

// Primero lo que importa: con ficheros y sin dar de alta.
filas.sort((a, b) => Number(a.dadaDeAlta) - Number(b.dadaDeAlta) || b.gpx - a.gpx);

const faltan = filas.filter(f => !f.dadaDeAlta && f.gpx > 0);
console.log(`%(prov)s: ${gpx.length} .gpx en ${carpetas.length} carpetas (${$input.all().length} páginas)`);
if (faltan.length) {
  console.log('  FALTAN POR DAR DE ALTA:');
  for (const f of faltan) console.log(`    ${f.carpeta} — ${f.gpx} .gpx — ${f.carpetaId}`);
} else {
  console.log('  no falta ninguna con ficheros');
}

return filas.map(json => ({ json }));
"""

nodos = [
    {"name": "Empezar", "type": "n8n-nodes-base.manualTrigger", "typeVersion": 1,
     "position": [-620, -120], "parameters": {}},
    # El mismo inventario, disparable desde fuera. Contesta al recibir y sigue por su
    # cuenta: lo que interesa queda en los registros de la ejecución, no en la
    # respuesta.
    {"name": "Disparar desde fuera", "type": "n8n-nodes-base.webhook", "typeVersion": 2,
     "position": [-620, 60], "webhookId": "inventario-carpetas-gpx",
     "parameters": {"path": "inventario-carpetas-gpx", "httpMethod": "POST",
                    "responseMode": "onReceived", "options": {}}},
    http("Carpetas de Rutas", -400, 0, {
        "url": f"{RUTAS}/api/ingest/folders", "sendHeaders": True,
        "headerParameters": {"parameters": [{"name": "x-api-key", "value": CLAVE}]}}),
    {"name": "Las de Rutas", "type": "n8n-nodes-base.code", "typeVersion": 2,
     "position": [-180, 0], "parameters": {"mode": "runOnceForAllItems", "jsCode": LAS_DE_RUTAS}},
]

conexiones = {
    "Empezar": {"main": [[{"node": "Carpetas de Rutas", "type": "main", "index": 0}]]},
    "Disparar desde fuera": {"main": [[{"node": "Carpetas de Rutas", "type": "main", "index": 0}]]},
    "Carpetas de Rutas": {"main": [[{"node": "Las de Rutas", "type": "main", "index": 0}]]},
}

salidas = []
y = -560
for prov, cred in PROVINCIAS.items():
    n1, n2, n3 = f"Carpetas de {prov}", f"GPX de {prov}", f"Qué falta en {prov}"
    nodos += [
        http(n1, 60, y, {"url": DRIVE, "options": PAGINAR, **consulta(
            ("q", f"'me' in owners and mimeType='{CARPETA}' and trashed=false"),
            ("fields", "nextPageToken,files(id,name,parents)"),
            ("pageSize", "1000"))}, cred=(cred, prov)),
        http(n2, 280, y, {"url": DRIVE, "options": PAGINAR, **consulta(
            ("q", "'me' in owners and name contains '.gpx' and trashed=false"),
            ("fields", "nextPageToken,files(id,parents)"),
            ("pageSize", "1000"))}, cred=(cred, prov)),
        {"name": n3, "type": "n8n-nodes-base.code", "typeVersion": 2, "position": [500, y],
         "parameters": {"mode": "runOnceForAllItems", "jsCode": CUENTA % {"prov": prov}}},
    ]
    conexiones[n1] = {"main": [[{"node": n2, "type": "main", "index": 0}]]}
    conexiones[n2] = {"main": [[{"node": n3, "type": "main", "index": 0}]]}
    salidas.append({"node": n1, "type": "main", "index": 0})
    y += 140

conexiones["Las de Rutas"] = {"main": [salidas]}

cuerpo = {"name": "Inventario: carpetas con .gpx sin dar de alta",
          "nodes": nodos, "connections": conexiones,
          "settings": {"executionOrder": "v1"}}

WORKFLOW = "aVy7ywMG7dIw2iJu"

if __name__ == "__main__":
    clave = sys.argv[1]
    r = subprocess.run(
        ["curl", "-s", "-m", "40", "-X", "PUT",
         "-H", f"X-N8N-API-KEY: {clave}", "-H", "content-type: application/json",
         "-d", json.dumps(cuerpo), f"{N8N}/workflows/{WORKFLOW}"],
        capture_output=True, text=True)
    try:
        w = json.loads(r.stdout)
        print("actualizado:", w.get("name"), "| id:", w.get("id"), "| nodos:", len(w.get("nodes", [])))
    except Exception:
        print("respuesta:", r.stdout[:500])
