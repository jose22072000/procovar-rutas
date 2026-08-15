#!/bin/sh
# Container startup for the routes panel.
#
# Migrations are applied here, on every start, before anything is brought up. The
# deployment cannot depend on someone remembering to log into the server and run
# `migrate`: the first time round the database did not even exist and the container
# sat in a restart loop with "database does not exist", which from the outside looks
# like a 502 and says nothing.
#
# `migrate up` is idempotent and golang-migrate takes a Postgres lock while it
# works, so if `api` and `ingest` start at the same time one waits for the other
# instead of stepping on it.
#
# A failed migration aborts on purpose: better a container that does not come up
# than one serving requests against a schema that does not match the code.
set -e

/usr/local/bin/migrate up

# The real command (api, or ingest with its flags) replaces this script, so it
# receives Docker's stop signals directly.
exec "$@"
