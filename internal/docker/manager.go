package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultImage    = "ubuntu:24.04"
	defaultUser     = "user"
	defaultPassword = "pass"
	defaultPort     = "2222"
)

type Manager struct {
	containerName string
	hostPort      string
	username      string
	password      string
	wrapperPath   string
}

type Status struct {
	Running       bool
	ContainerID   string
	StartedAt     string
	Uptime        string
	SSHCommand    string
	Username      string
	Password      string
	ContainerName string
	HostPort      string
}

func NewManager() *Manager {
	return &Manager{hostPort: defaultPort}
}

func (m *Manager) CheckDocker() error {
	return exec.Command("docker", "version", "--format", "{{.Server.Version}}").Run()
}

func (m *Manager) Defaults() (string, string) {
	return defaultUser, defaultPassword
}

func (m *Manager) Start(username, password string) error {
	if strings.TrimSpace(username) == "" {
		username = defaultUser
	}
	if strings.TrimSpace(password) == "" {
		password = defaultPassword
	}

	m.username = username
	m.password = password
	m.hostPort = defaultPort
	m.containerName = fmt.Sprintf("ephemeral-linux-%d", time.Now().Unix())

	if err := exec.Command("docker", "pull", defaultImage).Run(); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	args := []string{
		"run", "-d", "--rm",
		"--name", m.containerName,
		"-p", "127.0.0.1:" + m.hostPort + ":22",
		defaultImage,
		"bash", "-lc", startupScript(username, password),
	}
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start container: %w\n%s", err, string(output))
	}

	if err := m.writeWrapper(); err != nil {
		_ = m.Stop()
		return err
	}
	return nil
}

func (m *Manager) Stop() error {
	if m.wrapperPath != "" {
		_ = os.Remove(m.wrapperPath)
		m.wrapperPath = ""
	}
	if m.containerName == "" {
		return nil
	}
	cmd := exec.Command("docker", "stop", "-t", "0", m.containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(output, []byte("No such container")) {
			return nil
		}
		return fmt.Errorf("stop container: %w", err)
	}
	return nil
}

func (m *Manager) Status() (Status, error) {
	if m.containerName == "" {
		return Status{Username: m.username, Password: m.password, HostPort: m.hostPort, SSHCommand: m.sshCommand()}, nil
	}

	format := "{{.Id}}|{{.State.Running}}|{{.State.StartedAt}}"
	output, err := exec.Command("docker", "container", "inspect", "--format", format, m.containerName).CombinedOutput()
	if err != nil {
		return Status{}, fmt.Errorf("inspect container: %w", err)
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	status := Status{
		ContainerName: m.containerName,
		HostPort:      m.hostPort,
		SSHCommand:    m.sshCommand(),
		Username:      m.username,
		Password:      m.password,
	}
	if len(parts) > 0 {
		status.ContainerID = shortID(parts[0])
	}
	if len(parts) > 1 {
		status.Running = parts[1] == "true"
	}
	if len(parts) > 2 {
		status.StartedAt = parts[2]
		if t, err := time.Parse(time.RFC3339Nano, parts[2]); err == nil {
			status.Uptime = time.Since(t).Round(time.Second).String()
		}
	}
	return status, nil
}

func (m *Manager) sshCommand() string {
	if m.wrapperPath != "" {
		return m.wrapperPath
	}
	return fmt.Sprintf("ssh %s@localhost -p %s", m.username, m.hostPort)
}

func (m *Manager) writeWrapper() error {
	baseDir := filepath.Join(os.TempDir(), "ephemeral-linux")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create wrapper dir: %w", err)
	}
	m.wrapperPath = filepath.Join(baseDir, m.containerName+"-ssh")
	content := fmt.Sprintf("#!/usr/bin/env bash\nexec ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s@localhost -p %s \"$@\"\n", shellArg(m.username), shellArg(m.hostPort))
	if err := os.WriteFile(m.wrapperPath, []byte(content), 0o700); err != nil {
		return fmt.Errorf("write ssh wrapper: %w", err)
	}
	return nil
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func startupScript(username, password string) string {
	return fmt.Sprintf(`set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update >/dev/null
apt-get install -y openssh-server sudo >/dev/null
mkdir -p /var/run/sshd
useradd -m -s /bin/bash %[1]s || true
echo '%[1]s:%[2]s' | chpasswd
usermod -aG sudo %[1]s || true
printf 'PasswordAuthentication yes\nPermitRootLogin no\nUsePAM yes\n' >/etc/ssh/sshd_config
exec /usr/sbin/sshd -D -e`, shellArg(username), shellArg(password))
}

func shellArg(v string) string {
	return strings.ReplaceAll(v, "'", "")
}
