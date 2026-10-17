"""Rewrite PieFed's hard-coded https:// to the harness scheme.

PieFed 1.8.5 writes https:// into its own actor IDs and into the URLs it
fetches from other servers, whatever HTTP_PROTOCOL says. The harness federates
over plain HTTP. This rewrites https:// prefixes in f-strings and in
'https://' + ... concatenations, except S3 media URLs, to _HARNESS_SCHEME,
which sitecustomize sets from HTTP_PROTOCOL.
"""
import pathlib
import re
import sys

FSTRING_ANY = re.compile(r"""https://\{""")
CONCAT = re.compile(r"""(?<=[\s=(,])(["'])https://\1\s*\+""")

changed = 0
for path in pathlib.Path(sys.argv[1]).rglob("*.py"):
    lines = path.read_text().splitlines(keepends=True)
    out = []
    for line in lines:
        new = line
        if "S3_PUBLIC_URL" not in line:
            if re.search(r"""\bf["']""", line):
                new = FSTRING_ANY.sub("{_HARNESS_SCHEME}://{", new)
            new = CONCAT.sub("_HARNESS_SCHEME + '://' +", new)
        changed += new != line
        out.append(new)
    path.write_text("".join(out))
print(f"rewrote {changed} lines")
