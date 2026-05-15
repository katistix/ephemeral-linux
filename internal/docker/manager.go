package docker

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/katistix/ephemeral-linux/internal/config"
)

type Manager struct {
	cfg config.Config
}

type Status struct {
	ContainerExists bool
	Running         bool
	ContainerID     string
	StartedAt       string
	Uptime          string
	HostPort        int
	SSHCommand      string
	IPAddress       string
}

func NewManager(cfg config.Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) CheckDocker() error {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) EnsureImage() error {
	cmd := exec.Command("docker", "image", "inspect", m.cfg.ImageName)
	if err := cmd.Run(); err == nil {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "ephemeral-linux-build-*")
	if err != nil {
		return fmt.Errorf("create temp build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}

	build := exec.Command("docker", "build", "-t", m.cfg.ImageName, tmpDir)
	output, err := build.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build image %q: %w\n%s", m.cfg.ImageName, err, string(output))
	}
	return nil
}

func (m *Manager) EnsureContainerRunning() error {
	status, err := m.Status()
	if err != nil {
		return err
	}
	if !status.ContainerExists {
		return m.runContainer()
	}
	if !status.Running {
		return m.StartContainer()
	}
	return nil
}

func (m *Manager) runContainer() error {
	args := []string{
		"run", "-d",
		"--name", m.cfg.ContainerName,
		"-p", fmt.Sprintf("%d:22", m.cfg.HostPort),
		"-v", fmt.Sprintf("%s:/workspace", m.cfg.WorkspaceDir),
		"-e", fmt.Sprintf("SSH_USERNAME=%s", m.cfg.Username),
		"-e", fmt.Sprintf("SSH_PASSWORD=%s", m.cfg.Password),
		m.cfg.ImageName,
	}
	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run container: %w\n%s", err, string(output))
	}
	return nil
}

func (m *Manager) StartContainer() error {
	cmd := exec.Command("docker", "container", "start", m.cfg.ContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("start container: %w\n%s", err, string(output))
	}
	return nil
}

func (m *Manager) StopContainer() error {
	cmd := exec.Command("docker", "container", "stop", m.cfg.ContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stop container: %w\n%s", err, string(output))
	}
	return nil
}

func (m *Manager) RestartContainer() error {
	cmd := exec.Command("docker", "container", "restart", m.cfg.ContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart container: %w\n%s", err, string(output))
	}
	return nil
}

func (m *Manager) Logs(lines int) (string, error) {
	cmd := exec.Command("docker", "container", "logs", "--tail", fmt.Sprintf("%d", lines), m.cfg.ContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker logs: %w", err)
	}
	return string(output), nil
}

func (m *Manager) Status() (Status, error) {
	format := "{{.Id}}|{{.State.Status}}|{{.State.StartedAt}}|{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}"
	cmd := exec.Command("docker", "container", "inspect", "--format", format, m.cfg.ContainerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("No such object")) || bytes.Contains(output, []byte("No such container")) {
			return Status{HostPort: m.cfg.HostPort, SSHCommand: m.sshCommand()}, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Status{}, fmt.Errorf("inspect container: %s", strings.TrimSpace(string(output)))
		}
		return Status{}, fmt.Errorf("inspect container: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	status := Status{
		ContainerExists: true,
		HostPort:        m.cfg.HostPort,
		SSHCommand:      m.sshCommand(),
	}
	if len(parts) > 0 {
		status.ContainerID = shortID(parts[0])
	}
	if len(parts) > 1 {
		status.Running = parts[1] == "running"
	}
	if len(parts) > 2 {
		status.StartedAt = parts[2]
		if t, err := time.Parse(time.RFC3339Nano, parts[2]); err == nil && status.Running {
			status.Uptime = time.Since(t).Round(time.Second).String()
		}
	}
	if len(parts) > 3 {
		status.IPAddress = parts[3]
	}
	return status, nil
}

func (m *Manager) sshCommand() string {
	return fmt.Sprintf("ssh %s@localhost -p %d", m.cfg.Username, m.cfg.HostPort)
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

const dockerfile = `FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends openssh-server sudo ca-certificates curl git vim less procps iproute2 \
    && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /var/run/sshd /workspace
RUN printf 'PasswordAuthentication yes\nPermitRootLogin no\nUsePAM yes\n' >> /etc/ssh/sshd_config
RUN cat <<'EOF' >/usr/local/bin/start-sshd.sh
#!/usr/bin/env bash
set -euo pipefail

user="${SSH_USERNAME:-ephemeral}"
pass="${SSH_PASSWORD:-ephemeral}"

if ! id "$user" >/dev/null 2>&1; then
  useradd -m -s /bin/bash "$user"
fi

echo "$user:$pass" | chpasswd
usermod -aG sudo "$user"
mkdir -p /workspace
chown "$user:$user" /workspace

exec /usr/sbin/sshd -D -e
EOF
RUN chmod +x /usr/local/bin/start-sshd.sh

WORKDIR /workspace
EXPOSE 22
CMD ["/usr/local/bin/start-sshd.sh"]
`
