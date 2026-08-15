#!/bin/sh
# Arranque de los contenedores del panel de rutas.
#
# Las migraciones se aplican aquí, en cada arranque, antes de levantar nada. El
# despliegue no puede depender de que alguien se acuerde de entrar al servidor a
# lanzar `migrar`: la primera vez la base ni siquiera existía y el contenedor se
# quedó reiniciándose en bucle con "database does not exist", que desde fuera se
# ve como un 502 y no dice nada.
#
# `migrar up` es idempotente y golang-migrate toma un candado de Postgres
# mientras trabaja, así que si `api` e `ingesta` arrancan a la vez uno espera al
# otro en vez de pisarse.
#
# Si la migración falla se aborta a propósito: es preferible que el contenedor no
# levante a que sirva peticiones contra un esquema que no corresponde al código.
set -e

/usr/local/bin/migrar up

# El comando real (api, o ingesta con sus opciones) sustituye a este script, para
# que reciba las señales de parada de Docker directamente.
exec "$@"
