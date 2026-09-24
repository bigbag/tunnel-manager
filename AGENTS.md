# tunnel-manager

Go 1.25.5 CLI. It starts one named list of SSH and kubectl tunnels from `tunnels.json` and keeps those tunnels up.

This file applies to the whole repository. A user instruction overrides this file.

## Commands

```bash
make test
make vet
make lint
make build
make run/service
make run/service LIST=mcp ARGS='--check-interval 60'
```

Run one package with `go test ./internal/config -count=1`. Replace the package path with the package you change.

Do not run `make run/service` unless the user asks. The command asks for a password and holds the terminal.

## Layout

- `cmd/tunnel-manager` parses arguments, reads the password, and handles signals.
- `internal/config` loads `tunnels.json`.
- `internal/forward` runs the SSH client and the kubectl process.
- `internal/monitor` checks health and reconnects.
- `internal/run` starts the list, waits, and stops the tunnels.

Put a test next to the code it checks. Use the Go `testing` package. Do not add a new internal package for one helper.

## Rules

Follow KISS/DRY principles. Keep solutions simple.
If code clearly describes what it does, do not add comments.
Match existing style, even if you would do it differently.
Write all documentation, comments, and docstrings in Simplified Technical English (ASD-STE100). Use one idea per sentence. Use active voice and present tense. Use approved words in their approved meaning. Do not use filler or cliches.

## Boundaries

- Do not edit `tunnels.json` unless the user asks. That file is the operator catalog.
- Do not print, commit, or store passwords or private keys.
- Do not add a dependency unless the user asks.
- Do not add a CLI framework or a log library.
- Do not call a live SSH server or a live Kubernetes cluster from a test.
- Do not turn host-key checks on unless the user asks.
- Do not treat JSON `null` as an omitted field. `null` does not use `defaults`.
- When a CLI flag or an operator message changes, update `README.md` and `docs/business.md` in the same change.

## Docs

- `README.md` tells a person how to build and run the program.
- `docs/business.md` records the behavior and the tunnel catalog.
