#!/usr/bin/env python3
"""Crea "GPS Procesados" dentro de cada carpeta de vendedor. De una sola vez.

Con la credencial de SU provincia, no con la de la cuenta padre: así la subcarpeta
queda con el mismo dueño que la carpeta y que los ficheros, y apartar un fichero es
moverlo dentro de la misma cuenta —no un trasiego entre dueños distintos.

Sólo toca las carpetas que el panel tiene dadas de alta; el resto del Drive de cada
provincia (PEDIDOS, Errores, lo que sea) se queda como está.

Se puede volver a correr cuando entre un vendedor nuevo: no duplica nada, porque
antes de crear mira quién la tiene ya.

Uso:  python3 crear-procesados.py <clave-api-n8n>
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
    "PalmaSoriano": "gXTBf4gP9aNcVwLj",
    "Moa": "NFqUFPLVd9EAM1Fg",
}


def http(nombre, x, y, params, cred=None, on_error=None):
    p = {"options": {}}
    if cred:
        p["authentication"] = "predefinedCredentialType"
        p["nodeCredentialType"] = "googleDriveOAuth2Api"
    p.update(params)
    n = {"name": nombre, "type": "n8n-nodes-base.httpRequest", "typeVersion": 4.2,
         "position": [x, y], "parameters": p}
    if cred:
        n["credentials"] = {"googleDriveOAuth2Api": {"id": cred[0], "name": cred[1]}}
    if on_error:
        n["onError"] = on_error
    return n


def consulta(*pares):
    return {"sendQuery": True,
            "queryParameters": {"parameters": [{"name": k, "value": v} for k, v in pares]}}


LAS_DE_RUTAS = """
// Las carpetas de vendedor que el panel tiene dadas de alta, en un solo ítem: si se
// dejan sueltas, cada provincia de abajo se dispararía una vez por carpeta.
return [{ json: { ids: $input.all().map(i => i.json.folderId) } }];
"""

FALTAN = """
// De las carpetas de esta cuenta, las que están dadas de alta en el panel y todavía
// no tienen su "GPS Procesados".
const alta = new Set($('Las de Rutas').first().json.ids);
const mias = $('Carpetas de %(prov)s').first().json.files || [];

const conDestino = new Set();
for (const f of ($json.files || [])) {
  for (const p of (f.parents || [])) conDestino.add(p);
}

return mias
  .filter(c => alta.has(c.id) && !conDestino.has(c.id))
  .map(c => ({ json: { carpetaId: c.id, carpetaNombre: c.name } }));
"""

nodos = [
    {"name": "Empezar", "type": "n8n-nodes-base.manualTrigger", "typeVersion": 1,
     "position": [-620, -120], "parameters": {}},
    # Disparable desde fuera: hay que volver a correrlo cada vez que se da de alta
    # una carpeta, y esperar a que alguien se acuerde de pulsar el botón es la forma
    # de que unas carpetas aparten y otras no.
    {"name": "Disparar desde fuera", "type": "n8n-nodes-base.webhook", "typeVersion": 2,
     "position": [-620, 60], "webhookId": "crear-gps-procesados",
     "parameters": {"path": "crear-gps-procesados", "httpMethod": "POST",
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
    n1, n2, n3, n4 = (f"Carpetas de {prov}", f"Ya la tienen {prov}",
                      f"Faltan {prov}", f"Crear en {prov}")
    nodos += [
        http(n1, 60, y, {"url": DRIVE, **consulta(
            ("q", f"'me' in owners and mimeType='{CARPETA}' and trashed=false"),
            ("fields", "files(id,name)"),
            ("pageSize", "1000"))}, cred=(cred, prov)),
        http(n2, 280, y, {"url": DRIVE, **consulta(
            ("q", f"name='GPS Procesados' and 'me' in owners and mimeType='{CARPETA}' and trashed=false"),
            ("fields", "files(id,parents)"),
            ("pageSize", "1000"))}, cred=(cred, prov)),
        {"name": n3, "type": "n8n-nodes-base.code", "typeVersion": 2, "position": [500, y],
         "parameters": {"mode": "runOnceForAllItems", "jsCode": FALTAN % {"prov": prov}}},
        http(n4, 720, y, {
            "method": "POST", "url": DRIVE,
            **consulta(("fields", "id,name"), ("supportsAllDrives", "true")),
            "sendBody": True, "specifyBody": "json",
            "jsonBody": "={{ JSON.stringify({ name: 'GPS Procesados', mimeType: '" + CARPETA + "', parents: [$json.carpetaId] }) }}",
            "options": {"response": {"response": {"neverError": True}}},
        }, cred=(cred, prov), on_error="continueRegularOutput"),
    ]
    conexiones[n1] = {"main": [[{"node": n2, "type": "main", "index": 0}]]}
    conexiones[n2] = {"main": [[{"node": n3, "type": "main", "index": 0}]]}
    conexiones[n3] = {"main": [[{"node": n4, "type": "main", "index": 0}]]}
    salidas.append({"node": n1, "type": "main", "index": 0})
    y += 140

conexiones["Las de Rutas"] = {"main": [salidas]}

cuerpo = {"name": "Crear GPS Procesados en cada carpeta (repetible)",
          "nodes": nodos, "connections": conexiones,
          "settings": {"executionOrder": "v1"}}

# El flujo ya está publicado; se actualiza en su sitio para no dejar copias sueltas
# que luego no se sabe cuál es la buena.
WORKFLOW = "PanWPaQ7qEDEDmgq"

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
