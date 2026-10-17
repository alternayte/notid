# PieFed with its hard-coded https:// rewritten for the plain-HTTP harness;
# see patch.py. compileall fails the build if a rewrite breaks the syntax.
FROM ghcr.io/dockur/piefed:1.8.5
COPY sitecustomize.py /app/interop/sitecustomize.py
COPY patch.py /tmp/patch.py
ENV PYTHONPATH=/app/interop
RUN python /tmp/patch.py /app/app && python -m compileall -q /app/app > /dev/null
