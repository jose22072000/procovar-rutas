# Ingesta de rutas por n8n

Los vendedores suben su `.gpx` a Drive una vez al día. n8n vigila las carpetas y manda
cada fichero nuevo a la API de rutas, igual que ya hace con los pedidos.

## Qué hay aquí

Los flujos **no se editan a mano en la interfaz**: se construyen con estos guiones, que
son la versión buena. Son dieciséis y treinta y cinco nodos con expresiones largas; a
mano se copia mal uno y el fallo aparece tres pasadas después.

| Guion | Qué deja en n8n |
|---|---|
| `construir-ingesta.py` | *Ingesta Rutas GPX* — la pasada cada 12 h. Sustituye el flujo existente, conservando su id |
| `crear-procesados.py` | *Crear GPS Procesados en cada carpeta* — a mano, cuando entren vendedores nuevos |

```
python3 construir-ingesta.py <clave-api-n8n>
```

## Cómo sabe la ingesta lo que ya está hecho

Se lo pregunta al panel: `POST /api/ingest/known` recibe los ficheros de una carpeta y
contesta cuáles ya están dentro. Es una llamada por carpeta y con eso la pasada se salta
el histórico entero.

Antes se deducía de dónde estaba el fichero en Drive —lo procesado se apartaba, lo suelto
era lo pendiente—, y eso ataba la ingesta a un permiso que no había: las carpetas se
comparten desde la cuenta de cada provincia, y con permiso de solo lectura la cuenta padre
no podía mover nada. Cada pasada volvía a bajarse mil y pico ficheros.

Apartar sigue haciéndose, pero ya sólo es orden: aunque fallara, no se repite trabajo.

## Cómo queda una pasada

1. Las carpetas dadas de alta en el panel, y de quién es cada una.
2. Los `.gpx` de cada una, y cuáles conoce ya el panel.
3. **Los conocidos** se apartan a `GPS Procesados` de un tirón, sin bajarse nada.
4. **Los nuevos** se bajan de uno en uno, se mandan y se apartan — con un tope de 300 por
   pasada (`TOPE_NUEVOS`), para que una pasada termine siempre. Lo que sobra entra en la
   siguiente y el flujo lo dice en el registro.

Un fichero que la API rechaza **se queda donde está**, a propósito: así la pasada
siguiente lo reintenta en vez de esconderlo en una carpeta que nadie mira.

## `GPS Procesados`, y por qué la crea cada provincia

`crear-procesados.py` la crea con la credencial de **su** provincia, no con la de la
cuenta padre. Así la subcarpeta tiene el mismo dueño que la carpeta y que los ficheros, y
apartar es mover dentro de la misma cuenta, no un trasiego entre dueños distintos.

Sólo toca las carpetas dadas de alta en el panel; el resto del Drive de cada provincia se
queda como está. Se puede volver a correr cuando entre un vendedor nuevo: mira quién la
tiene ya, así que no duplica.

Si una carpeta no la tiene, no se rompe nada — sus ficheros se ingestan igual y
simplemente no se apartan. El flujo lo deja escrito en el registro.

## Los dos caminos, y por qué hay dos

| | Quién trae los bytes | Cuándo |
|---|---|---|
| **n8n** (este flujo) | n8n descarga y empuja a `POST /api/ingest/file` | cada 12 h |
| **Barrido propio** (`cmd/ingest`) | el propio servicio lee Drive | cada 30 min + repaso nocturno |

Los dos acaban en la misma tabla y **son idempotentes** por `driveFileId` y por `sha256`:
se pueden dejar los dos encendidos sin duplicar nada. Conviene, de hecho — n8n es el
camino rápido y el barrido nocturno es la red que recoge lo que n8n se haya perdido.

## Puesta en marcha

1. La **credencial de Google** de la cuenta padre (`tablets.procovar`) ya está en n8n; los
   guiones la referencian por id.
2. `SERVICE_API_KEY` de la API de rutas es la clave que llevan los guiones.
3. Dar de alta las carpetas en **Administración**. n8n manda el `folderId`, y la API lo
   rechaza si esa carpeta no está dada de alta: es a propósito, para que un fichero no
   aparezca bajo una fuente fantasma sin sucursal ni zona horaria.
4. Correr `crear-procesados.py` y ejecutar ese flujo una vez.
5. Ejecutar la ingesta a mano y revisar la **bandeja**: ahí cae lo que no se haya podido
   asignar (perfiles con nombre de tableta, sobre todo). Se casan una vez y ya no vuelven
   a preguntar.

## Detalles que importan

**La API contesta 202 y encola.** El procesado —parsear miles de puntos, recalcular el
día— ocurre después, en el servicio de ingesta, leyendo de la cola de Redis
(`procovar-rutas:ingesta:*`). Así, aunque toda la plantilla suba a la misma hora, n8n no
se queda esperando ni el panel se resiente.

**Para depurar**, añadir `?sync=1` a la URL: procesa en el acto y devuelve el resultado
real, igual que en `/orders/bulk` de PEDIDO.

**El permiso de escritura** de la cuenta padre sobre las carpetas de cada provincia se dio
una vez, desde la credencial de cada una (108 carpetas). Si aparece una provincia nueva,
hay que repetirlo antes de que apartar funcione ahí.
