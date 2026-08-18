#!/usr/bin/env python3
"""Da permiso de ESCRITURA a la cuenta padre sobre las carpetas de cada provincia.

Hay que correrlo cuando entra una provincia nueva o cuando alguien crea carpetas.

Cada cuenta comparte lo SUYO: una cuenta no puede repartir permisos sobre carpetas de
otra, así que esto no se puede hacer desde la cuenta padre —que es lo que se intentó
primero—. Se hace desde la credencial de cada provincia, sobre sus propias carpetas.

Sin este permiso la ingesta no puede apartar los ficheros procesados, y entonces cada
pasada vuelve a mirar el histórico entero.

El correo de la cuenta padre no se escribe a mano: se le pregunta a Drive con su
propia credencial, que es la única forma de no equivocarse al teclearlo.

Uso:  python3 dar-permiso-padre.py <clave-api-n8n>
"""
import json
import subprocess
import sys

N8N = "https://n8n.procovar.cloud/api/v1"
DRIVE = "https://www.googleapis.com/drive/v3/files"
CARPETA = "application/vnd.google-apps.folder"
PADRE = {"id": "YHaaPvwunOWiPSIN", "name": "Cuenta padre"}

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


def http(nombre, x, y, params, cred, on_error=None):
    p = {
        "authentication": "predefinedCredentialType",
        "nodeCredentialType": "googleDriveOAuth2Api",
        "options": {},
    }
    p.update(params)
    n = {"name": nombre, "type": "n8n-nodes-base.httpRequest", "typeVersion": 4.2,
         "position": [x, y], "parameters": p,
         "credentials": {"googleDriveOAuth2Api": {"id": cred[0], "name": cred[1]}}}
    if on_error:
        n["onError"] = on_error
    return n


def consulta(*pares):
    return {"sendQuery": True,
            "queryParameters": {"parameters": [{"name": k, "value": v} for k, v in pares]}}


nodos = [
    {"name": "Empezar", "type": "n8n-nodes-base.manualTrigger", "typeVersion": 1,
     "position": [-620, -120], "parameters": {}},
    {"name": "Disparar desde fuera", "type": "n8n-nodes-base.webhook", "typeVersion": 2,
     "position": [-620, 60], "webhookId": "dar-permiso-padre",
     "parameters": {"path": "dar-permiso-padre", "httpMethod": "POST",
                    "responseMode": "onReceived", "options": {}}},
    # Quién es la cuenta padre: se le pregunta a Drive con su propia credencial, en
    # vez de escribir un correo a mano y equivocarse.
    http("Quién es el padre", -380, 0, {
        "url": "https://www.googleapis.com/drive/v3/about",
        **consulta(("fields", "user")),
    }, (PADRE["id"], PADRE["name"])),
]

conexiones = {
    "Empezar": {"main": [[{"node": "Quién es el padre", "type": "main", "index": 0}]]},
    "Disparar desde fuera": {"main": [[{"node": "Quién es el padre", "type": "main", "index": 0}]]},
}

salidas = []
y = -620
for prov, cred in PROVINCIAS.items():
    n1, n2, n3 = f"Carpetas de {prov}", f"Una por carpeta {prov}", f"Dar permiso {prov}"
    nodos += [
        http(n1, -120, y, {"url": DRIVE, **consulta(
            ("q", f"'me' in owners and mimeType='{CARPETA}' and trashed=false"),
            ("fields", "files(id,name)"),
            ("pageSize", "1000"))}, (cred, prov)),
        {"name": n2, "type": "n8n-nodes-base.splitOut", "typeVersion": 1,
         "position": [120, y], "parameters": {"fieldToSplitOut": "files", "options": {}}},
        http(n3, 360, y, {
            "method": "POST",
            "url": "={{ 'https://www.googleapis.com/drive/v3/files/' + $json.id + '/permissions' }}",
            **consulta(("sendNotificationEmail", "false"), ("supportsAllDrives", "true")),
            "sendBody": True, "specifyBody": "json",
            "jsonBody": ("={{ JSON.stringify({ role: 'writer', type: 'user', "
                         "emailAddress: $('Quién es el padre').first().json.user.emailAddress }) }}"),
            "options": {"response": {"response": {"neverError": True}}},
        }, (cred, prov), on_error="continueRegularOutput"),
    ]
    conexiones[n1] = {"main": [[{"node": n2, "type": "main", "index": 0}]]}
    conexiones[n2] = {"main": [[{"node": n3, "type": "main", "index": 0}]]}
    salidas.append({"node": n1, "type": "main", "index": 0})
    y += 130

conexiones["Quién es el padre"] = {"main": [salidas]}

cuerpo = {"name": "Dar permiso de edición a la cuenta padre (repetible)",
          "nodes": nodos, "connections": conexiones,
          "settings": {"executionOrder": "v1"}}

WORKFLOW = "4d1poxQCXPWDAnCd"

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
