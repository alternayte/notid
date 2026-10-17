#!/usr/bin/env bash
# The image entrypoint runs populate_community_search on every boot, which
# fetches lemmyverse.net. The harness has no outbound DNS, so run the rest of
# the entrypoint directly.
set -euo pipefail

export FLASK_APP=pyfedi.py

flask db upgrade
exec gunicorn --config gunicorn.conf.py --preload pyfedi:app
