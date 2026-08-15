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

# CGO off and statically linked, so it can run on a minimal image.
# The migrations live inside the binary (go:embed), so there are no .sql files to
# copy separately and they cannot drift from the code.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/api      ./cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/ingest   ./cmd/ingest && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/migrate  ./cmd/migrate

FROM alpine:3.21

# tzdata is essential, not an extra: without the time zone database
# America/Havana does not exist and the 9:00–16:00 workday would be computed in
# UTC. The reports would come out four hours off.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 rutas

COPY --from=construccion /bin/api /bin/ingest /bin/migrate /usr/local/bin/
COPY entrypoint.sh /usr/local/bin/entrypoint
RUN chmod +x /usr/local/bin/entrypoint

USER rutas
EXPOSE 3600

# Everything goes through `entrypoint`, which migrates the database before handing
# over to the command. It works the same for `ingest`, which overrides CMD but not
# ENTRYPOINT.
ENTRYPOINT ["/usr/local/bin/entrypoint"]
CMD ["/usr/local/bin/api"]
