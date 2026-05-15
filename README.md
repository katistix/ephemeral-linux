# ephemeral-linux

A small Go TUI for launching and monitoring a local ephemeral Linux Docker container with SSH access.

## Features

- single command starts the TUI and ensures the container is running
- Bubble Tea dashboard with tabs for overview, logs, and access
- scrollable logs viewport with keyboard navigation
- richer observability: state, timestamps, CPU, memory, PID count, SSH details, and paths
- simple YAML config generated at `~/.config/ephemeral-linux/config.yaml`
- local SSH credentials shown in the TUI when needed
- local workspace mounted into the container at `/workspace`

## Run

```bash
go run ./cmd/ephemeral-linux
```

Or build it:

```bash
go build -o ephemeral-linux ./cmd/ephemeral-linux
./ephemeral-linux
```

## Requirements

- Docker installed and running

## Default SSH access

- host: `localhost`
- port: `2222`
- user: `ephemeral`
- password: `ephemeral`

SSH command:

```bash
ssh ephemeral@localhost -p 2222
```

## Config

On first run the app creates:

- `~/.config/ephemeral-linux/config.yaml`
- `~/.config/ephemeral-linux/workspace/`

Example config:

```yaml
container_name: "ephemeral-linux"
image_name: "ephemeral-linux:latest"
host_port: 2222
username: "ephemeral"
password: "ephemeral"
workspace_dir: "/home/you/.config/ephemeral-linux/workspace"
```

## TUI keys

- `tab` / `shift+tab`: switch tabs
- `r`: refresh
- `c`: show/hide credentials
- `s`: start/stop container
- `R`: restart container
- `↑` / `↓`, `PgUp` / `PgDn`, `g` / `G`: scroll logs
- `q`: quit
