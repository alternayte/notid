# Loaded by every PieFed process in the harness through PYTHONPATH.
import builtins
import os
import sys
import types

# The Dockerfile rewrites PieFed's hard-coded https:// f-strings to this name.
builtins.__dict__["_HARNESS_SCHEME"] = os.environ.get("HTTP_PROTOCOL", "https")

# PieFed's init-db reads the admin password through pwinput, which needs a tty.
# The harness has no tty, so a stand-in module reads the password from stdin.
# sitecustomize runs before app.cli imports pwinput.
_pwinput = types.ModuleType("pwinput")


def _read(prompt="Password: ", mask="*"):
    return input(prompt)


_pwinput.__dict__.update(pwinput=_read)
sys.modules["pwinput"] = _pwinput
