# Ingesta de rutas por n8n

Los vendedores suben su `.gpx` a Drive una vez al día. n8n vigila las carpetas y manda
cada fichero nuevo a la API de rutas, igual que ya hace con los pedidos.

## Los dos caminos, y por qué hay dos

| | Quién trae los bytes | Cuándo |
|---|---|---|
| **n8n** (este flujo) | n8n descarga y empuja a `POST /api/ingesta/fichero` | cada hora |
| **Barrido propio** (`cmd/ingesta`) | el propio servicio lee Drive | cada 30 min + repaso nocturno |

Los dos acaban en la misma tabla y **son idempotentes** por `driveFileId` y por
`sha256`: se pueden dejar los dos encendidos sin duplicar nada. Conviene, de hecho —
n8n es el camino rápido y el barrido nocturno es la red que recoge lo que n8n se haya
perdido (una ejecución fallida, un fichero renombrado, un día que el flujo estuvo
apagado).

Si prefieres solo n8n, basta con no arrancar `cmd/ingesta --demonio` y lanzar el barrido
a mano de vez en cuando desde Administración.

## Puesta en marcha

1. Importar `ingesta-rutas-gpx.json` en n8n.
2. **Asignar la credencial de Google** a los tres nodos de Drive (Buscar, Carpeta del
   perfil, Descargar). La cuenta es **`tablets.procovar`**, la cuenta padre que tiene
   compartidas las carpetas de todas las sucursales.
3. Poner `RUTAS_API_KEY` en el entorno de n8n, con el mismo valor que `SERVICE_API_KEY`
   de la API de rutas.
4. Dar de alta las carpetas en la pantalla **Administración** del panel. n8n manda el
   `folderId`, y la API lo rechaza si esa carpeta no está dada de alta: es a propósito,
   para que un fichero no aparezca bajo una fuente fantasma sin sucursal ni zona horaria.
5. Ejecutar el flujo a mano una vez y revisar la **bandeja**: ahí caerá lo que no se
   haya podido asignar (perfiles con nombre de tableta, sobre todo). Se casan una vez y
   ya no vuelven a preguntar.

## Detalles que importan

**La ventana de búsqueda es de 3 días.** Podría ser de una hora, pero entonces un
fichero que llegue con retraso o una ejecución fallida dejarían un agujero permanente.
Como la API deduplica, re-mandar lo mismo no cuesta nada.

**La API contesta 202 y encola.** El procesado —parsear miles de puntos, recalcular el
día— ocurre después, en el servicio de ingesta, leyendo de la cola de Redis
(`procovar-rutas:ingesta:*`). Así, aunque toda la plantilla suba a la misma hora, n8n no
se queda esperando ni el panel se resiente.

**Para depurar**, añadir `?sync=1` a la URL: procesa en el acto y devuelve el resultado
real, igual que en `/orders/bulk` de PEDIDO.

**Este flujo no mueve ni borra nada del Drive**, a diferencia del de pedidos. Esas
carpetas las ven los propios trabajadores, y el registro de "ya procesé esto" vive en la
base.
