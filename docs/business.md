# Tunnel manager

The program starts one named list from `tunnels.json`. It keeps those tunnels up until the operator stops the process.

A tunnel is an SSH local port forward or a `kubectl port-forward`.

## Command

```
tunnel-manager <list_name> [options]
tunnel-manager --list
tunnel-manager -l
```

`make run/service` starts list `mcp`. Set `LIST` and `ARGS` to change that command.

- `--no-monitor` does not run health checks or reconnect. The monitor is on by default.
- `--check-interval N` sets the seconds between health-check passes. `N` is a float. The default is `30`.
- `--check-timeout N` sets the TCP connect timeout for one health check, in seconds. `N` is a float. The default is `5`.
- `--max-retries N` sets the maximum reconnect attempts before give-up. `N` is an int. The default is `5`.
- `-h` and `--help` print usage and exit 0.
- `-l` and `--list` print list names and tunnel counts, then exit.

Rules:

- `--list` and `-l` work only as the first argument. Other arguments are ignored in that case.
- `-h` and `--help` work in any other position. They exit 0 before the config load.
- A missing list name prints usage and exits 1.
- An unknown option prints the option, prints usage, and exits 1.
- A flag without a value is an unknown option.
- A bad number prints `Error: invalid value for <flag>: <value>` and exits 1.
- The program accepts any parsed number. This includes `0` and a negative number.
- A `--check-timeout` of `0` or less fails the health check at once.
- When the operator gives more than one list name, the last name is used.
- Flags can follow the list name.

Exit codes:

- Config or argument errors exit 1.
- A normal stop exits 0.
- A stop after 2 rejected passwords exits 1.
- If no tunnel reaches status `running`, the process prints `No tunnels started successfully.` and exits 1.

## Configuration file

Path: `tunnels.json` in the current directory. The path is not a flag.

- A missing file prints `Error: Config file not found: tunnels.json` and exits 1.
- Invalid JSON or a schema error prints `Error loading config: <error>` and exits 1.
- An unknown list prints `Error: List '<name>' not found`, a blank line, then the available lists, and exits 1.
- A list name that is not in `tunnels` prints `Warning: tunnel '<name>' not found in config` and skips that name.
- An empty resolved list prints `Error: List '<name>' is empty` and exits 1.

`--list` prints:

```
Available lists:
  <name> (<count> tunnels)
```

The count is the number of names in the list. It is not the number of names that resolve.

Root fields:

- `version` is optional. The default is `1.0`. The runner stores it and does not use it.
- `defaults` is optional. It holds SSH fallback values.
- `lists` is optional. It maps a list name to tunnel names. A missing map means an empty map.
- `tunnels` is required. It is an array of tunnel objects.

`name`, `type`, `local_port`, and `remote_port` are required. A missing port is not port `0`. `description` and `tags` are stored. The runner does not use them.

SSH fields: `ssh_user`, `ssh_bastion`, `identity_file`, `auth`, `remote_host`.

Kubectl fields: `context`, `namespace`, `service`, `address`.

- An absent field uses the default. For `remote_host`, the default is `127.0.0.1`.
- JSON `null` does not use the default. The value stays empty.
- A string, including `""`, is used as written.

`auth` is `"key"` or `"password"`. `defaults` has no `auth`. An absent or `null` `auth` is `"key"`. Another value is a schema error.

There is no default for kubectl fields. JSON `null` or an absent `address` omits `--address`.

Duplicate tunnel names are allowed. The last definition wins. A repeated name in one list is resolved twice. The second start fails because that tunnel is already running.

The current file has one list, `mcp`: `mra_auth`, `mcp_account`, `rds_sdip_prod`, `embeddings`, `rag`, `starrock_mcp`, `sks`.

## Password

The process asks for passwords before it starts tunnels. It asks one time for each `<ssh_user>@<ssh_bastion>` pair of an SSH tunnel with `auth` `password` in the selected list. It asks in list order. If no tunnel in the list uses `password`, the process does not ask.

```
Password for <ssh_user>@<ssh_bastion>:
```

The user and host come from the tunnel. If a value is empty, the prompt uses `user` and `SSH server`.

The password is hidden. Tunnels with the same user and host use the same password. Later reconnects use the same password. An empty password is sent as typed. The process does not ask again.

If the terminal cannot be opened, the process prints an error and exits 1. It does not start tunnels.

The process counts auth errors for each user and host only after it attempts a password. If the server does not offer password authentication, the failure does not count. An SSH disconnect with `too many authentication failures` after a password attempt also counts as an auth error. A network error does not count as an auth error. The count does not reset. At 2 errors, the process:

- logs `Password for <ssh_user>@<ssh_bastion> was rejected 2 times. Stopping all tunnels.`
- does not send that password again. A later start of a tunnel with that user and host fails with `Password for <ssh_user>@<ssh_bastion> was rejected 2 times. It is not sent again.` The start does not open a connection.
- stops all tunnels as in Shutdown, and exits 1.

If the stop occurs during the start loop, the loop stops before the next tunnel. If it occurs during a monitor pass, that pass stops before the next tunnel.

## Start

The process prints this banner before the password prompt. The count is the resolved list length.

```
SSH Tunnel Runner - Starting '<list>' (<count> tunnels)
```

Tunnels start one by one, in list order.

Success:

```
[<name>] OK - localhost:<local_port> -> <remote>
```

Failure:

```
[<name>] FAILED - <error_message>
```

Remote text is `<remote_host>:<remote_port>` for SSH and `<service>:<remote_port>` for kubectl.

A failed tunnel does not stop the other starts. After the loop, the process prints the running count and `Press Ctrl+C to stop.`

SIGINT and SIGTERM are armed before the start loop. If a signal arrives during start, the process does not start another tunnel. If at least one tunnel is `running`, the process stops the tunnels. If no tunnel is `running`, it exits 1 and does not run shutdown.

## SSH

The program uses a Go SSH client. It does not run the `ssh` binary.

- The bastion port is `22`.
- SSH authentication has a 30-second timeout. This includes the TCP connection, agent requests, and SSH handshake.
- Host-key check is off.
- Keepalive is every 15 seconds. Each reply has a 45-second timeout. A timeout or request error closes the SSH connection and its local listener.
- With `auth` `key`, a set key file is used when the file exists. A missing file logs `Key not found: <path>` and the start continues.
- With `auth` `key`, the agent is used when `SSH_AUTH_SOCK` is set and the socket opens. If the agent cannot list keys, the client can still use the key file. The agent connection closes after authentication. Cancellation also closes a pending agent connection.
- With `auth` `key`, one public-key authentication method tries the key file, then the agent keys. One agent client serves all public-key authentication steps on the connection. No password is sent.
- With `auth` `password`, only the password is sent. The key file and the agent are not used.
- The listener binds all interfaces on `local_port`.
- The destination is `remote_host:remote_port` as seen from the bastion.
- Status becomes `running` only after the listener is open.
- The forward log says `localhost`. The socket is not limited to localhost.

## Kubectl

The program runs `kubectl` from `PATH`.

```
kubectl port-forward --namespace=<namespace> --context=<context> [--address=<address>] service/<service> <local_port>:<remote_port>
```

Start returns success when the process has a PID. Status becomes `running` before the local port is open.

Stop sends SIGTERM, waits 5 seconds, then sends SIGKILL. This wait is not `--check-timeout`.

A non-zero exit sets status `error`.

## Monitor

The monitor starts when monitoring is on and at least one tunnel is `running`.

It checks status `running` and status `error`. It skips `stopped`, `starting`, and `stopping`.

The health test opens a TCP connection to `127.0.0.1:<local_port>`. The timeout is `--check-timeout`. Checks are sequential.

Restart means stop, then start. The first unhealthy pass retries at once.

Attempts `1` through `--max-retries` call start. The next attempt gives up for the rest of the process. A later healthy check clears that state.

Backoff seconds are `min(5 * (2 ** (attempt - 1)), 300)`. A retry runs only on a check pass, and only when the backoff time has passed.

## Shutdown

SIGINT, SIGTERM, or 2 rejected passwords cancel startup and the monitor. Cancellation closes connections used for pending SSH authentication, including the agent connection. The process waits for the monitor to return before it stops tunnels. A current TCP health check must return first. The process then stops every tunnel whose status is not `stopped`. This includes status `error`. It prints `Done.` It exits 0 after a signal and exits 1 after 2 rejected passwords.

## Limits

- The program does not edit `tunnels.json`.
- It does not start a tunnel that the selected list does not name.
- It does not share one SSH connection across tunnels.
- It does not read `~/.ssh/config`.
- It does not verify host keys.
- It does not import shell scripts.

## Catalog

Defaults:

- user: `user`
- bastion: `192.168.8.166`
- key: `/home/work/.ssh/id_ed25519`

An omitted SSH field uses these defaults. JSON `null` does not.

SSH tunnels:

- `embeddings` forwards local `8081` to `127.0.0.1:8081`. The key is set to the default key. It is in list `mcp`.
- `mcp` forwards local `8000` to `127.0.0.1:8000`. It is not in a list.
- `sks` forwards local `8200` to `127.0.0.1:8200`. It is in list `mcp`.
- `mcp_account` forwards local `9000` to `127.0.0.1:9000`. It is in list `mcp`.
- `mra_auth` forwards local `8001` to `127.0.0.1:8001`. It is in list `mcp`.
- `mra_web` forwards local `8181` to `127.0.0.1:8181`.
- `pd_sdip_dev` forwards local `6432` to `10.104.161.114:5432`.
- `pg_ad_prod` forwards local `5432` to `10.101.32.81:5432`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `pg_ad_prod_replica` forwards local `5432` to `10.101.33.113:5432`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `pg_changelog` forwards local `5432` to `prod-spm-us-east-1-changelog-rds-0.c1rcypkoolf2.us-east-1.rds.amazonaws.com:5432`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `pg_dev_seal` forwards local `6432` to `10.103.43.27:6432`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `pg_dev_shark` forwards local `6432` to `10.103.36.71:5432`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `pg_dev_turtle` forwards local `6432` to `10.103.34.169:6432`.
- `pg_gateway_aws` forwards local `6432` to `127.0.0.1:5432`.
- `pg_gateway_prod` forwards local `6432` to `127.0.0.1:5432`.
- `pg_ml_dev` forwards local `6432` to `10.103.41.109:6432`.
- `pg_ml_prod` forwards local `5432` to `127.0.0.1:5432`.
- `pg_mra` forwards local `6432` to `127.0.0.1:5432`.
- `pg_mra_agent` forwards local `6432` to `127.0.0.1:5432`.
- `pg_sks_dev` forwards local `5432` to `127.0.0.1:5432`.
- `pg_sks_prod` forwards local `6432` to `127.0.0.1:5432`.
- `rag` forwards local `8484` to `127.0.0.1:8484`. The key is set to the default key. It is in list `mcp`.
- `rds_sdip_dev` forwards local `5432` to `rdssdipinstance.cdmwwwue0ooy.eu-west-1.rds.amazonaws.com:5432`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `rds_sdip_prod` forwards local `5432` to `prod-sdip-recommendations-rds-01.cbgcqeymsfvy.eu-west-1.rds.amazonaws.com:5432`. It is in list `mcp`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `redis_sks_dev` forwards local `6378` to `127.0.0.1:6379`.
- `selenium` forwards local `4444` to `127.0.0.1:4444`.
- `service_gateway` forwards local `8080` to `127.0.0.1:8000`.
- `service_ml_dev` forwards local `8004` to `10.103.41.109:8005`.
- `service_ml_prod` forwards local `8004` to `10.101.37.32:8005`.
- `service_sea` forwards local `8106` to `127.0.0.1:8106`.
- `service_sks` forwards local `8501` to `127.0.0.1:8501`.
- `starrock_mcp` forwards local `9030` to `127.0.0.1:9030`. It is in list `mcp`. The key is JSON `null`, so the default key is not used. Auth is `password`.
- `windows` forwards local `3389` to `192.168.2.110:3389`. The bastion is `10.200.200.3`. The key is JSON `null`, so the default key is not used. Auth is `password`.

Kubectl tunnels:

- `pg_aki__airflow_dev` uses context `aki-dev-master`, namespace `aki-dev-master`, and service `aki-dev-master-db-airflow`. Local `5433` maps to remote `5432`. The address is `0.0.0.0`.
- `pg_aki__airflow_prod` uses context `aki-prod-master`, namespace `aki-prod-master`, and service `aki-prod-master-db-airflow`. Local `5433` maps to remote `5432`. The address is `0.0.0.0`.
- `pg_aki_dev` uses context `aki-dev-master`, namespace `aki-dev-master`, and service `db-app-postgresql`. Local `5432` maps to remote `5432`. It has no address.
- `pg_aki_prod` uses context `aki-prod-master`, namespace `aki-prod-master`, and service `db-app-postgresql`. Local `5432` maps to remote `5432`. The address is `0.0.0.0`.
- `pg_gateway_do_prod` uses context `aki-prod-master`, namespace `asa-gateway-prod`, and service `gateway-postgresql`. Local `6432` maps to remote `5432`. The address is `0.0.0.0`.
- `service_exporter_dev` uses context `sks-dev-master`, namespace `hq-to-sea-turtle`, and service `hq-to-sea-exporter`. Local `8205` maps to remote `8205`. It has no address.
- `service_exporter_prod` uses context `sks-prod-master`, namespace `hq-to-sea-production`, and service `hq-to-sea-exporter`. Local `8205` maps to remote `8506`. It has no address.

Catalog size: 40 tunnels. 33 SSH. 7 kubectl.
