# Imagen de la API y la ingesta.
#
# Los tres ejecutables van en la misma imagen y se elige con el `command` del
# servicio: comparten código y no tiene sentido construir tres veces lo mismo.

FROM golang:1.26-alpine AS construccion

WORKDIR /src

# Las dependencias primero: mientras go.mod y go.sum no cambien, esta capa se
# reutiliza y la compilación de un cambio de código baja a segundos.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO apagado y estático, para poder correr sobre una imagen mínima.
# Las migraciones van dentro del binario (go:embed), así que no hay que copiar
# los .sql aparte ni pueden desincronizarse del código.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/api      ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/ingesta  ./cmd/ingesta && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/migrar   ./cmd/migrar

FROM alpine:3.21

# tzdata es imprescindible, no un extra: sin la base de zonas horarias,
# America/Havana no existe y la jornada 9:00–16:00 se calcularía en UTC. Los
# reportes saldrían corridos cuatro horas.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 rutas

COPY --from=construccion /bin/api /bin/ingesta /bin/migrar /usr/local/bin/
COPY entrada.sh /usr/local/bin/entrada
RUN chmod +x /usr/local/bin/entrada

USER rutas
EXPOSE 3600

# Todo pasa por `entrada`, que migra la base antes de ceder el paso al comando.
# Vale igual para `ingesta`, que sobrescribe el CMD pero no el ENTRYPOINT.
ENTRYPOINT ["/usr/local/bin/entrada"]
CMD ["/usr/local/bin/api"]
