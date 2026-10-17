# PieFed's init-db reads the admin password through pwinput, which needs a tty.
# The harness has no tty, so a stand-in module reads the password from stdin.
# sitecustomize runs before app.cli imports pwinput.
import sys
import types

_pwinput = types.ModuleType("pwinput")


def _read(prompt="Password: ", mask="*"):
    return input(prompt)


_pwinput.__dict__.update(pwinput=_read)
sys.modules["pwinput"] = _pwinput
