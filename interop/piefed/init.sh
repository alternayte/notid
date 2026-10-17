#!/usr/bin/env bash
# Prepares PieFed's database and its admin user, then exits. It runs once per
# volume: init-db drops every table, so it must not run on a live instance.
set -euo pipefail

export FLASK_APP=pyfedi.py

flask db upgrade

db_url="${DATABASE_URL/postgresql+psycopg2/postgresql}"
users="$(psql "${db_url}" -tAc 'SELECT count(*) FROM "user"')"
if [ "${users}" = "0" ]; then
  printf 'admin\nadmin@piefed.test\nlemmylemmy\n' | flask init-db
fi
