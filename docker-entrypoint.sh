#!/bin/sh
# Start the API, optionally importing the provider workbooks first.
#
# The server applies migrations on startup by itself; this only decides whether the workbooks
# are loaded before it begins serving.
set -e

# The importer is idempotent -- upsert on primary key plus a sweep, with every table's rows
# content-hashed -- so running it again is safe. It is still opt-in rather than automatic on
# every boot, because it takes long enough that a platform health check would fail the deploy
# while it worked, and a restart under load would delay serving for no gain.
#
# Set IMPORT_ON_START=1 for a first deploy against an empty database, then remove it.
if [ -n "$IMPORT_ON_START" ]; then
    echo "IMPORT_ON_START set: loading provider workbooks before serving"
    import
    echo "import complete"
fi

exec server
