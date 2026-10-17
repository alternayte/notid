# Interop harness

`just interop` starts a Compose stack and runs the tests in this directory
against it. Everything federates over plain HTTP.

| Host | What runs there |
| --- | --- |
| `a.notid.test:8080` | notid installation A (app domain) |
| `b.notid.test:8080` | notid installation B |
| `woodwork.club:8080` | a community domain; CoreDNS points it at A or B |
| `lemmy.test:8536` | Lemmy 0.19.20 |
| `piefed.test:5000` | PieFed 1.8.5 |
| `mailpit` | mail catcher; A and B send sign-in links here |

- Hosts resolve through the CoreDNS container from `coredns/hosts`. The move
  test rewrites `interop/.run/hosts`, and CoreDNS picks it up within 5 s.
- Every container gets a globally scoped bridge address. Lemmy and PieFed both
  refuse to federate with private addresses.
- Lemmy release builds refuse http URLs at compile time, so `lemmy/build.sh`
  builds Lemmy from source once with the default debug profile. The first run
  is slow and needs a few GB of disk.
- The image also pairs an arm64 toolchain that needs glibc 2.38 with a bookworm
  runner that has 2.36, so `build.sh` uses a trixie runner on arm64.
- PieFed's image entrypoint imports the lemmyverse community list on every boot.
  The harness has no outbound DNS, so `piefed/run.sh` skips that step.
- PieFed's `init-db` reads the admin password with a tty-only prompt;
  `piefed/sitecustomize.py` reads it from stdin instead.

Ports published on the host for the tests: A 28080, B 28081, Lemmy 28536,
PieFed 25000, mailpit 28025.

The tests reach the harness by its real host names and dial the published
loopback ports, so cookies and origins match what the peers see. `just interop`
starts from empty volumes, because the move tests change community state.
