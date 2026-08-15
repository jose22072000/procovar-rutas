# procovar-rutas

Control de las rutas GPS de los vendedores a partir de los `.gpx` que suben a las
carpetas de Google Drive. **Aplicación administrativa**: el vendedor no entra.

Hace tres cosas:

1. **Guarda el histórico.** Ingiere los `.gpx` de las carpetas de Drive y los mete en
   Postgres para siempre; a partir de ahí se consultan sin volver a tocar Drive.
2. **Es el visor.** Se toca un vendedor, se elige un día y se ve por dónde estuvo, sin
   descargar nada ni abrir ninguna aplicación externa de GPX.
3. **Avisa solo.** Calendario de lunes a viernes con los tres casos que hay que cazar:
   *sin fichero*, *fichero sin fechas* y *día sin moverse del sitio*.

Plan completo: [`../PLAN-RUTAS-GPX.md`](../PLAN-RUTAS-GPX.md)

## Cómo están los datos en Drive

Hay una cuenta de Google por sucursal, pero todas las carpetas están compartidas además
en la cuenta padre **`tablets.procovar`**: con esa sola credencial se ven todas.

```
Cuenta padre tablets.procovar      ← una sola credencial lo ve todo
└── Carpeta de rutas de Granma     ← se da de alta como fuente
    ├── Alexander/                 ← una carpeta por perfil de GPS
    │   ├── 20260810.gpx           ← un fichero por día
    │   └── 20260811.gpx
    └── TAB-CMG-04/                ← a veces el perfil es la tableta, no la persona
        └── 20260810.gpx
```

El nombre del fichero solo trae la fecha, así que **el vendedor sale de la carpeta**.
Cuando el perfil es el nombre de una tableta, el fichero cae en la **bandeja**, un admin
lo casa una vez, y a partir de ahí los de ese dispositivo se resuelven solos.

## Estructura

```
api/                     Go — la API y la ingesta
  cmd/api/               servidor HTTP (chi)
  cmd/ingesta/           barrido de Drive: incremental, nocturno y backfill
  cmd/migrar/            aplica el esquema (golang-migrate empotrado)
  cmd/autorizar/         consigue el token de refresco de una cuenta de Google
  internal/gpx/          parser de .gpx y resolución fichero → vendedor
  internal/metricas/     el veredicto del día: km, paradas, cobertura, estado
  internal/alcance/      quién puede ver los recorridos de quién
  internal/ingesta/      el barrido y el recálculo de días
  internal/api/          manejadores HTTP
  internal/reporte/      el documento semanal
  internal/almacen/      acceso a datos (sqlc)
  internal/cola/         cola en Redis de lo que empuja n8n
  migraciones/           *.sql versionados, empotrados en el binario
n8n/                     flujo que recoge los .gpx y los manda a la API
front/                   Next.js + Leaflet — solo interfaz, sin acceso a la base
  app/                   calendario · visor · bandeja · administración · reporte
```

Toda la lógica que decide algo vive en `internal/` y se prueba sin base de datos.

## Puesta en marcha

```bash
export PATH=$HOME/sdk/go/bin:$PATH        # si Go no está en el PATH del sistema
cd api
cp .env.example .env                      # y rellenarlo

go test ./...                             # las de integración se saltan sin base

export DATABASE_URL='postgres://usuario:clave@localhost:5432/procovar_rutas?sslmode=disable'
go run ./cmd/migrar up                    # aplica el esquema
go run ./cmd/api                          # panel en :3600
go run ./cmd/ingesta --backfill           # primera carga del histórico
go run ./cmd/ingesta --demonio            # y a partir de ahí, solo

cd ../front && npm install && npm run dev # interfaz en :3601
```

Conectar una cuenta de Google (una por sucursal):

```bash
go run ./cmd/autorizar -clave granma -client-id … -client-secret …
```

Imprime el objeto que hay que añadir a `GOOGLE_CUENTAS`. El permiso es de **solo
lectura**.

### Pruebas con base de datos

```bash
docker run -d --rm --name rutas-pg -e POSTGRES_PASSWORD=prueba -p 55432:5432 postgres:16-alpine
DATABASE_URL='postgres://postgres:prueba@127.0.0.1:55432/postgres?sslmode=disable' go run ./cmd/migrar up
DATABASE_URL_TEST='postgres://postgres:prueba@127.0.0.1:55432/postgres?sslmode=disable' go test ./...
```

## Decisiones que conviene conocer antes de tocar el código

**Las horas son `TIMESTAMPTZ` y la jornada se recorta con `AT TIME ZONE
'America/Havana'`.** Nunca se suman horas a mano: Cuba tiene horario de verano y los
reportes de marzo y noviembre saldrían corridos justo en las semanas en que a nadie se
le ocurre revisarlos.

**`track_day` existe aunque no haya fichero.** Un proceso nocturno crea la fila de cada
vendedor activo por cada día laborable; si no llegó nada, queda en `SIN_FICHERO`. Si la
ausencia fuese «no hay fila», no se podría listar, ni contar, ni ordenar por número de
faltas — que es justo lo que pide el calendario.

**La inmovilidad se decide por el radio de dispersión, no por los kilómetros.** Un
teléfono quieto encima de una mesa acumula varios kilómetros al día de puro temblor del
GPS; si el criterio fueran los kilómetros, ese día se colaría como trabajado. El radio
no se corrompe: si en siete horas los puntos nunca se alejaron 300 m de su centro, esa
persona no hizo una ruta. Está en `internal/metricas/dia.go`, con sus pruebas.

**Hay dos caminos de entrada y los dos valen a la vez.** n8n empuja los ficheros a
`POST /api/ingesta/fichero` (se encolan en Redis con prefijo `procovar-rutas:` y los
procesa el servicio de ingesta), y además el barrido propio lee Drive por su cuenta.
Ambos son idempotentes por `driveFileId` y `sha256`, así que pueden convivir: n8n es el
camino rápido y el barrido nocturno la red de seguridad. Ver [`n8n/`](n8n/).

**El barrido no tiene prisa, pero el repaso nocturno no se salta.** El incremental cada
30 minutos es barato; el repaso completo diario es el que garantiza que no falte nada
aunque un fichero llegue renombrado, movido o con la fecha cambiada.

**Los ficheros de Drive no se mueven ni se borran.** La ingesta de PEDIDO sí los mueve a
`Procesados/`, pero estas carpetas las ven los propios trabajadores. El registro de «ya
procesé esto» vive en `gpx_file`, por `drive_file_id` y `sha256`.

**Nadie ve su propio recorrido**, y el alcance del supervisor se evalúa contra la fecha
consultada, no contra hoy (tabla `supervision`, con vigencias). Está en
`internal/alcance` y es lo único que toda consulta debe atravesar.

## Roles

| Rol | Qué ve | Administra |
|---|---|---|
| `super_admin` | todas las sucursales | sí |
| `admin` | su sucursal completa | sí |
| `gerente` | su sucursal completa | **no** |
| `supervisor` | sus vendedores vigentes esa fecha | no |
| `gestor` | **sin acceso** | no |

El gerente está por encima del supervisor y ve toda su sucursal, pero no toca carpetas
de Drive, ni alias, ni umbrales.

## Estado

- [x] Parser de `.gpx`, resolución fichero → vendedor, motor del día, alcance por rol
- [x] Esquema y migraciones (aplicadas y revertidas contra Postgres real)
- [x] Acceso a datos con sqlc
- [x] Ingesta de Drive, multicuenta, con pruebas de extremo a extremo
- [x] API HTTP con sesión de procovar-auth
- [x] Frontend: calendario, visor, bandeja, administración, reporte semanal
- [x] Cola en Redis y puerta de servicio para n8n, con flujo listo para importar
- [ ] Probarlo con `.gpx` reales y con las carpetas de verdad
- [ ] Cruce de paradas con la geo de clientes (visitas probables)
- [ ] Despliegue en Dokploy
