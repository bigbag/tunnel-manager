# tunnel-manager

Start one named list of SSH and kubectl tunnels. Keep the tunnels up until you stop the process.

## Build

```bash
make build
make test
make install
```

The binary is `bin/tunnel-manager`.

## Run the service

`make run` builds the binary and starts list `mcp`.

```bash
make run/service
make run/service LIST=mcp ARGS='--check-interval 60'
make run/quick
```

`LIST` is the list name. The default is `mcp`. `ARGS` is extra flags. `run/quick` does not rebuild.

The process asks for one SSH password when the list contains an SSH tunnel. Press Ctrl+C to stop.

## Command

```
tunnel-manager <list_name> [options]
tunnel-manager --list
```

- `--no-monitor` disables health checks and reconnect. The monitor is on by default.
- `--check-interval N` sets the seconds between health checks. `N` is a float. The default is `30`.
- `--check-timeout N` sets the TCP connect timeout for one health check, in seconds. The default is `5`.
- `--max-retries N` sets the reconnect attempts before give-up. The default is `5`.
- `-h` and `--help` print usage and exit 0.
- `-l` and `--list` print list names. This flag works only as the first argument.

`--list` and `-l` ignore later arguments. Flags can follow the list name. A bad number exits 1. If no tunnel starts, the process exits 1.

## Configuration

The program reads `tunnels.json` from the current directory. It does not edit that file. Copy `tunnels.json.sample` to `tunnels.json` and edit the copy.

A list names the tunnels to start. The program does not start a tunnel that the list does not name.

An omitted SSH field uses `defaults`. JSON `null` does not use `defaults`. An omitted `remote_host` is `127.0.0.1`.

The tunnel catalog is in `docs/business.md`.

## Health

The monitor connects to `127.0.0.1:<local_port>`. A closed port starts the tunnel again. The backoff is 5, 10, 20, 40, and 80 seconds. The cap is 300 seconds. A retry runs on the next check pass.

SSH tunnels listen on all interfaces. The log line says `localhost`. Each SSH tunnel uses port 22 on the bastion. The client does not check host keys.
