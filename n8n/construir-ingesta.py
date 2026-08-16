#!/usr/bin/env python3
"""Deja el workflow de ingesta en n8n tal como debe estar.

Se escribe aquí, y no a mano en la interfaz, porque son dieciséis nodos con
expresiones largas: a mano se copia mal uno y el fallo aparece tres pasadas
después.

Uso:  python3 construir-ingesta.py <clave-api-n8n>
"""
import json
import subprocess
import sys

N8N = "https://n8n.procovar.cloud/api/v1"
WORKFLOW = "T6ofv7GHRkkdE8fM"
PADRE = {"id": "YHaaPvwunOWiPSIN", "name": "Cuenta padre"}
RUTAS = "https://rutas.procovar.cloud"
CLAVE = "14a657aaaf962ef033b1c508b6f6555f225a0c99d90f98358575d33e80f8cf98"
DRIVE = "https://www.googleapis.com/drive/v3/files"

# Cuántos ficheros SIN INGESTAR se baja como mucho una pasada. Los ya ingestados no
# cuentan: esos sólo se apartan, que es una llamada y ya.
#
# Es un freno de emergencia, no un ritmo: alto a propósito para que el atraso se vacíe
# de una vez en lugar de a trozos de 300 —una pasada tarda unos dos segundos por
# fichero, así que mil son media hora larga y se aguanta—. Con el atraso vaciado, aquí
# entran los treinta y pico ficheros del día y esto no vuelve a tocarse.
TOPE_NUEVOS = 3000


def cabecera_servicio():
    return {
        "sendHeaders": True,
        "headerParameters": {"parameters": [{"name": "x-api-key", "value": CLAVE}]},
    }


def http(nombre, x, y, params, drive=False, on_error=None):
    p = {"options": {}}
    if drive:
        p["authentication"] = "predefinedCredentialType"
        p["nodeCredentialType"] = "googleDriveOAuth2Api"
    p.update(params)
    n = {
        "name": nombre,
        "type": "n8n-nodes-base.httpRequest",
        "typeVersion": 4.2,
        "position": [x, y],
        "parameters": p,
    }
    if drive:
        n["credentials"] = {"googleDriveOAuth2Api": PADRE}
    if on_error:
        n["onError"] = on_error
    return n


def codigo(nombre, x, y, js):
    return {
        "name": nombre,
        "type": "n8n-nodes-base.code",
        "typeVersion": 2,
        "position": [x, y],
        "parameters": {"mode": "runOnceForAllItems", "jsCode": js},
    }


def consulta(*pares):
    return {
        "sendQuery": True,
        "queryParameters": {
            "parameters": [{"name": k, "value": v} for k, v in pares]
        },
    }


JUNTAR = """
// Un ítem por carpeta, con sus ficheros dentro.
//
// Los dos nodos de arriba corren una vez por carpeta y en el mismo orden, así que se
// emparejan por posición. Se hace aquí, y no más abajo, porque en cuanto los ítems
// pasan por un troceado o un bucle ese emparejamiento se pierde —y con él la cuenta,
// que es lo que dice de qué sucursal es la carpeta.
const duenos = $('Dueño de la carpeta').all();
const listas = $('GPX de la carpeta').all();
const salida = [];

for (let i = 0; i < listas.length; i++) {
  const d = (duenos[i] && duenos[i].json) || {};
  const dueno = (d.owners && d.owners[0]) || {};
  const ficheros = listas[i].json.files || [];
  if (!ficheros.length) continue;

  salida.push({
    json: {
      carpetaId: d.id,
      carpetaNombre: d.name,
      cuenta: dueno.displayName || dueno.emailAddress || '',
      ficheros,
    },
  });
}

return salida;
"""

PEGAR = """
// A cada carpeta, la lista de lo que el panel ya tiene.
const carpetas = $('Juntar por carpeta').all();
const respuestas = $input.all();

return carpetas.map((c, i) => ({
  json: {
    ...c.json,
    known: (respuestas[i] && respuestas[i].json.known) || [],
  },
}));
"""

REPARTIR = """
// De aquí sale un ítem por fichero, ya sabiendo qué hacer con él.
//
// Lo que el panel ya tiene no se vuelve a bajar: se aparta y ya. Eso es lo que
// convierte una pasada de mil y pico ficheros en una vuelta corta por las carpetas.
const TOPE = %d;

const carpetas = $('Pegar lo conocido').all();
const destinos = $input.all();
const salida = [];
let nuevos = 0, aplazados = 0, apartar = 0, sinDestino = [];

for (let i = 0; i < carpetas.length; i++) {
  const c = carpetas[i].json;
  const encontrada = ((destinos[i] && destinos[i].json.files) || [])[0];
  const destinoId = encontrada ? encontrada.id : '';
  if (!destinoId) sinDestino.push(c.carpetaNombre);

  const ya = new Set(c.known);
  for (const f of c.ficheros) {
    const yaEsta = ya.has(f.id);

    // Conocido y sin "GPS Procesados" donde meterlo: no hay nada que hacer con él.
    // No se pierde: la próxima pasada lo vuelve a dar por conocido, que cuesta nada.
    if (yaEsta && !destinoId) continue;

    if (!yaEsta) {
      if (nuevos >= TOPE) { aplazados++; continue; }
      nuevos++;
    } else {
      apartar++;
    }

    salida.push({
      json: {
        ...f,
        carpetaId: c.carpetaId,
        carpetaNombre: c.carpetaNombre,
        cuenta: c.cuenta,
        destinoId,
        yaEsta,
      },
    });
  }
}

console.log(`carpetas con ficheros: ${carpetas.length}`);
console.log(`por bajar ahora: ${nuevos}` + (aplazados ? ` — quedan ${aplazados} para la próxima pasada` : ''));
console.log(`sólo apartar (ya estaban dentro): ${apartar}`);
if (sinDestino.length) {
  console.log(`sin "GPS Procesados", no se aparta nada de: ${sinDestino.join(', ')}`);
}

return salida;
""" % TOPE_NUEVOS


nodos = [
    {
        "name": "Cada 12 horas",
        "type": "n8n-nodes-base.scheduleTrigger",
        "typeVersion": 1.2,
        "position": [-620, -120],
        "parameters": {"rule": {"interval": [{"field": "hours", "hoursInterval": 12}]}},
    },
    # La misma pasada, disparable desde fuera. Sirve para no esperar doce horas
    # cuando se acaba de dar de alta una carpeta. Contesta al recibir y sigue por su
    # cuenta: una pasada dura minutos y nadie va a tener la petición abierta.
    {
        "name": "Disparar desde fuera",
        "type": "n8n-nodes-base.webhook",
        "typeVersion": 2,
        "position": [-620, 60],
        "webhookId": "ingesta-rutas-gpx",
        "parameters": {"path": "ingesta-rutas-gpx", "httpMethod": "POST",
                       "responseMode": "onReceived", "options": {}},
    },
    http("Carpetas de Rutas", -400, 0, {"url": f"{RUTAS}/api/ingest/folders", **cabecera_servicio()}),
    http("Dueño de la carpeta", -180, 0, {
        "url": "={{ 'https://www.googleapis.com/drive/v3/files/' + $json.folderId }}",
        **consulta(("fields", "id,name,owners"), ("supportsAllDrives", "true")),
    }, drive=True),
    http("Decir de quién es", -180, 200, {
        "method": "POST", "url": f"{RUTAS}/api/ingest/folder-owner", **cabecera_servicio(),
        "sendBody": True, "specifyBody": "json",
        "jsonBody": "={{ JSON.stringify({ folderId: $json.id, account: ($json.owners && $json.owners[0] ? ($json.owners[0].displayName || $json.owners[0].emailAddress) : '') }) }}",
        "options": {"response": {"response": {"neverError": True}}},
    }, on_error="continueRegularOutput"),
    http("GPX de la carpeta", 40, 0, {
        "url": DRIVE,
        **consulta(
            ("q", "={{ \"'\" + $json.id + \"' in parents and name contains '.gpx' and trashed = false\" }}"),
            ("fields", "files(id,name,createdTime,size)"),
            ("pageSize", "1000"),
            ("supportsAllDrives", "true"),
            ("includeItemsFromAllDrives", "true"),
        ),
    }, drive=True),
    codigo("Juntar por carpeta", 260, 0, JUNTAR),
    # El nodo que lo cambia todo: en vez de deducir lo hecho de dónde está el fichero
    # en Drive, se le pregunta al panel, que es quien lo sabe.
    http("¿Cuáles ya están?", 480, 0, {
        "method": "POST", "url": f"{RUTAS}/api/ingest/known", **cabecera_servicio(),
        "sendBody": True, "specifyBody": "json",
        "jsonBody": "={{ JSON.stringify({ driveFileIds: $json.ficheros.map(f => f.id) }) }}",
    }),
    codigo("Pegar lo conocido", 700, 0, PEGAR),
    # Si una carpeta no tiene "GPS Procesados", no se crea aquí: se crea de una vez
    # para todas con la credencial de su provincia (ver crear-procesados.py), que es
    # la que deja la subcarpeta con el mismo dueño que la carpeta y sus ficheros.
    http("Buscar Procesados", 920, 0, {
        "url": DRIVE,
        **consulta(
            ("q", "={{ \"name='GPS Procesados' and '\" + $json.carpetaId + \"' in parents and mimeType='application/vnd.google-apps.folder' and trashed=false\" }}"),
            ("fields", "files(id)"),
            ("supportsAllDrives", "true"),
            ("includeItemsFromAllDrives", "true"),
        ),
    }, drive=True),
    codigo("Repartir ficheros", 1140, 0, REPARTIR),
    {
        "name": "¿Ya está dentro?",
        "type": "n8n-nodes-base.if",
        "typeVersion": 2,
        "position": [1360, 0],
        "parameters": {"conditions": {
            "options": {"caseSensitive": True, "version": 2},
            "combinator": "and",
            "conditions": [{
                "id": "ya",
                "operator": {"type": "boolean", "operation": "true", "singleValue": True},
                "leftValue": "={{ $json.yaEsta }}",
                "rightValue": "",
            }],
        }, "options": {}},
    },
    # Todos los conocidos se apartan en una sola ejecución de este nodo. Aquí es donde
    # se vacía el atraso: son llamadas de nada, sin bajarse un solo fichero.
    http("Apartar lo conocido", 1580, -140, {
        "method": "PATCH",
        "url": "={{ 'https://www.googleapis.com/drive/v3/files/' + $json.id }}",
        **consulta(
            ("addParents", "={{ $json.destinoId }}"),
            ("removeParents", "={{ $json.carpetaId }}"),
            ("fields", "id"),
            ("supportsAllDrives", "true"),
        ),
        "options": {"response": {"response": {"neverError": True, "fullResponse": True}}},
    }, drive=True, on_error="continueRegularOutput"),
    {
        "name": "Por fichero",
        "type": "n8n-nodes-base.splitInBatches",
        "typeVersion": 3,
        "position": [1580, 160],
        "parameters": {"batchSize": 1, "options": {"reset": False}},
    },
    {
        "name": "Descargar",
        "type": "n8n-nodes-base.googleDrive",
        "typeVersion": 3,
        "position": [1800, 260],
        "parameters": {"operation": "download", "fileId": {"__rl": True, "value": "={{ $json.id }}", "mode": "id"}, "options": {}},
        "credentials": {"googleDriveOAuth2Api": PADRE},
    },
    {
        "name": "A base64",
        "type": "n8n-nodes-base.extractFromFile",
        "typeVersion": 1,
        "position": [2020, 260],
        "parameters": {"operation": "binaryToPropery", "destinationKey": "contenidoBase64", "options": {}},
    },
    http("Enviar a Rutas", 2240, 260, {
        "method": "POST", "url": f"{RUTAS}/api/ingest/file", **cabecera_servicio(),
        "sendBody": True, "specifyBody": "json",
        "jsonBody": "={{ JSON.stringify({ account: $('Por fichero').item.json.cuenta, folderId: $('Por fichero').item.json.carpetaId, driveFileId: $('Por fichero').item.json.id, name: $('Por fichero').item.json.name, folderPath: [$('Por fichero').item.json.carpetaNombre], createdAt: $('Por fichero').item.json.createdTime, contentBase64: $json.contenidoBase64 }) }}",
        "options": {"response": {"response": {"neverError": True, "fullResponse": True}}},
    }),
    # Sólo se aparta lo que el panel aceptó. Un fichero que falló se queda donde está
    # a propósito: así la pasada siguiente lo reintenta en vez de esconderlo.
    {
        "name": "¿Entró bien?",
        "type": "n8n-nodes-base.if",
        "typeVersion": 2,
        "position": [2460, 260],
        "parameters": {"conditions": {
            "options": {"caseSensitive": True, "version": 2},
            "combinator": "and",
            "conditions": [
                {"id": "ok", "operator": {"type": "number", "operation": "gte"},
                 "leftValue": "={{ $json.statusCode }}", "rightValue": 200},
                {"id": "ok2", "operator": {"type": "number", "operation": "lt"},
                 "leftValue": "={{ $json.statusCode }}", "rightValue": 300},
                {"id": "destino", "operator": {"type": "string", "operation": "notEmpty", "singleValue": True},
                 "leftValue": "={{ $('Por fichero').item.json.destinoId }}", "rightValue": ""},
            ],
        }, "options": {}},
    },
    http("Apartar el nuevo", 2680, 160, {
        "method": "PATCH",
        "url": "={{ 'https://www.googleapis.com/drive/v3/files/' + $('Por fichero').item.json.id }}",
        **consulta(
            ("addParents", "={{ $('Por fichero').item.json.destinoId }}"),
            ("removeParents", "={{ $('Por fichero').item.json.carpetaId }}"),
            ("fields", "id"),
            ("supportsAllDrives", "true"),
        ),
        "options": {"response": {"response": {"neverError": True, "fullResponse": True}}},
    }, drive=True, on_error="continueRegularOutput"),
]

conexiones = {
    "Cada 12 horas": {"main": [[{"node": "Carpetas de Rutas", "type": "main", "index": 0}]]},
    "Disparar desde fuera": {"main": [[{"node": "Carpetas de Rutas", "type": "main", "index": 0}]]},
    "Carpetas de Rutas": {"main": [[{"node": "Dueño de la carpeta", "type": "main", "index": 0}]]},
    "Dueño de la carpeta": {"main": [[
        {"node": "Decir de quién es", "type": "main", "index": 0},
        {"node": "GPX de la carpeta", "type": "main", "index": 0},
    ]]},
    "GPX de la carpeta": {"main": [[{"node": "Juntar por carpeta", "type": "main", "index": 0}]]},
    "Juntar por carpeta": {"main": [[{"node": "¿Cuáles ya están?", "type": "main", "index": 0}]]},
    "¿Cuáles ya están?": {"main": [[{"node": "Pegar lo conocido", "type": "main", "index": 0}]]},
    "Pegar lo conocido": {"main": [[{"node": "Buscar Procesados", "type": "main", "index": 0}]]},
    "Buscar Procesados": {"main": [[{"node": "Repartir ficheros", "type": "main", "index": 0}]]},
    "Repartir ficheros": {"main": [[{"node": "¿Ya está dentro?", "type": "main", "index": 0}]]},
    "¿Ya está dentro?": {"main": [
        [{"node": "Apartar lo conocido", "type": "main", "index": 0}],
        [{"node": "Por fichero", "type": "main", "index": 0}],
    ]},
    "Por fichero": {"main": [[], [{"node": "Descargar", "type": "main", "index": 0}]]},
    "Descargar": {"main": [[{"node": "A base64", "type": "main", "index": 0}]]},
    "A base64": {"main": [[{"node": "Enviar a Rutas", "type": "main", "index": 0}]]},
    "Enviar a Rutas": {"main": [[{"node": "¿Entró bien?", "type": "main", "index": 0}]]},
    "¿Entró bien?": {"main": [
        [{"node": "Apartar el nuevo", "type": "main", "index": 0}],
        [{"node": "Por fichero", "type": "main", "index": 0}],
    ]},
    "Apartar el nuevo": {"main": [[{"node": "Por fichero", "type": "main", "index": 0}]]},
}

cuerpo = {
    "name": "Ingesta Rutas GPX — cuenta padre tablets.procovar",
    "nodes": nodos,
    "connections": conexiones,
    "settings": {"executionOrder": "v1"},
}

if __name__ == "__main__":
    clave = sys.argv[1]
    r = subprocess.run(
        ["curl", "-s", "-m", "40", "-X", "PUT",
         "-H", f"X-N8N-API-KEY: {clave}", "-H", "content-type: application/json",
         "-d", json.dumps(cuerpo), f"{N8N}/workflows/{WORKFLOW}"],
        capture_output=True, text=True)
    try:
        w = json.loads(r.stdout)
        print("actualizado:", w.get("name"), "| nodos:", len(w.get("nodes", [])))
    except Exception:
        print("respuesta:", r.stdout[:500])
