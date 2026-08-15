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

## Estructura

```
api/                     Go — la API y la ingesta
  cmd/api/               servidor HTTP (chi)
  cmd/ingesta/           barrido de Drive: incremental, nocturno y backfill
  cmd/migrar/            aplica el esquema (golang-migrate empotrado)
  internal/gpx/          parser de .gpx y resolución fichero → vendedor
  internal/metricas/     el veredicto del día: km, paradas, cobertura, estado
  internal/alcance/      quién puede ver los recorridos de quién
  internal/almacen/      acceso a datos (sqlc)
  migraciones/           *.sql versionados, empotrados en el binario
front/                   Next.js + Leaflet — solo interfaz, sin acceso a la base
```

Toda la lógica que decide algo vive en `internal/` y se prueba sin base de datos.

## Puesta en marcha

```bash
export PATH=$HOME/sdk/go/bin:$PATH        # si Go no está en el PATH del sistema
cd api

go test ./...                             # las pruebas no necesitan Postgres

export DATABASE_URL='postgres://usuario:clave@localhost:5432/procovar_rutas?sslmode=disable'
go run ./cmd/migrar up                    # aplica el esquema
go run ./cmd/migrar version               # en qué versión está la base
go run ./cmd/migrar down 1                # revierte la última
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
persona no hizo una ruta. Está en `internal/metricas/dia.go` y hay pruebas del caso.

**Los ficheros de Drive no se mueven ni se borran.** La ingesta de PEDIDO sí los mueve a
`Procesados/`, pero estas carpetas las ven los propios trabajadores. El registro de «ya
procesé esto» vive en la tabla `gpx_file`, por `drive_file_id` y `sha256`.

**Nadie ve su propio recorrido**, y el alcance del supervisor se evalúa contra la fecha
consultada, no contra hoy (tabla `supervision`, con vigencias). Ambas reglas están en
`internal/alcance` y son lo único que toda consulta debe atravesar.

## Estado

- [x] Parser de `.gpx`, resolución fichero → vendedor, motor del día, alcance por rol
- [x] Esquema y migraciones
- [ ] `internal/almacen` con sqlc
- [ ] Ingesta de Drive (hace falta acceso a las carpetas reales)
- [ ] API HTTP y SSO con procovar-auth
- [ ] Frontend: calendario y visor
- [ ] Reporte semanal
