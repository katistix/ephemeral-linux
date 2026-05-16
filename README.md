# ephemeral-linux

A tiny Bubble Tea app for spinning up a quick bare-bones Linux box I can SSH into from macOS to mimic my university OS exam server.

## Install

Go is required.

```bash
go install github.com/katistix/ephemeral-linux/cmd/ephlinux@latest
```

## Run

```bash
ephlinux
```

## What it does

- asks for SSH username and password on startup
- launches a fresh temporary container
- shows basic status and the SSH command
- removes the container when the TUI exits

## Requirements

- Docker installed and running

## Defaults

- user: `user`
- password: `pass`
- SSH port: `2222`

## Notes

- the container is temporary and is removed when you quit the TUI
- SSH setup happens inside the container at startup
- the app creates a temporary local SSH wrapper command so fresh host keys do not pollute `known_hosts`
- intended for local use only
