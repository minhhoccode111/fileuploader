#!/bin/sh
# Air restarts this script instead of the app, so a .env edit reloads the
# variables. exec keeps one process, so air's stop signal reaches the app.
set -a
[ -f .env ] && . ./.env
set +a

exec ./tmp/app
