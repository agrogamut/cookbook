#!/usr/bin/env fish
#
# Throwaway local Postgres for development and for the integrity suite.
#
#   scripts/dev_db.fish up      start a fresh container (destroys the old one)
#   scripts/dev_db.fish down    stop and remove it
#   scripts/dev_db.fish url     print the connection string
#   scripts/dev_db.fish psql    open a shell on it
#
# The container is deliberately disposable. Nothing of value lives in it: the workbooks
# are the source of truth and the importer rebuilds the database from them.

set -l name recipie-pg
set -l port 55432
set -l url "postgres://recipie:recipie@localhost:$port/recipie?sslmode=disable"

switch "$argv[1]"
    case up
        docker rm -f $name >/dev/null 2>&1
        docker run -d --name $name \
            -e POSTGRES_USER=recipie \
            -e POSTGRES_PASSWORD=recipie \
            -e POSTGRES_DB=recipie \
            -p $port:5432 \
            postgres:16-alpine >/dev/null
        or begin
            echo "could not start $name" >&2
            exit 1
        end

        for i in (seq 1 30)
            if docker exec $name pg_isready -U recipie >/dev/null 2>&1
                echo "ready: $url"
                exit 0
            end
            sleep 1
        end
        echo "$name did not become ready in 30s" >&2
        exit 1

    case down
        docker rm -f $name >/dev/null 2>&1
        echo "removed $name"

    case url
        echo $url

    case psql
        docker exec -it $name psql -U recipie

    case '*'
        echo "usage: scripts/dev_db.fish [up|down|url|psql]" >&2
        exit 2
end
