package docker

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultImage    = "ephemeral-linux-ssh:latest"
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
	SSHReady      bool
	ContainerID   string
	StartedAt     string
	Uptime        string
	SSHCommand    string
	Username      string
	Password      string
	ContainerName string
	HostPort      string
	Phase         string
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

func (m *Manager) ImageReady() bool {
	return exec.Command("docker", "image", "inspect", defaultImage).Run() == nil
}

func (m *Manager) PrepareImage() error {
	return m.ensureImage()
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

	if err := ensureNoRunningEphemeralLinux(); err != nil {
		return err
	}

	m.containerName = fmt.Sprintf("ephemeral-linux-%d", time.Now().Unix())

	args := []string{
		"run", "-d", "--rm",
		"--name", m.containerName,
		"-p", "127.0.0.1:" + m.hostPort + ":22",
		"-e", "EPHEMERAL_USER=" + username,
		"-e", "EPHEMERAL_PASSWORD=" + password,
		defaultImage,
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
	status.SSHReady = status.Running && sshdReady(m.containerName, m.hostPort)
	status.Phase = phaseText(status)
	return status, nil
}

func sshdReady(containerName, port string) bool {
	if strings.TrimSpace(port) == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()

	output, err := exec.Command("docker", "logs", containerName).CombinedOutput()
	if err != nil {
		return false
	}
	logs := string(output)
	return strings.Contains(logs, "Server listening on 0.0.0.0 port 22.") || strings.Contains(logs, "Server listening on :: port 22.")
}

func phaseText(status Status) string {
	switch {
	case !status.Running:
		return "container stopped"
	case !status.SSHReady:
		return "container running, ssh starting"
	default:
		return "ssh ready"
	}
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
	content := fmt.Sprintf("#!/usr/bin/env bash\nexec ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s@localhost -p %s \"$@\"\n", shellWrap(m.username), shellWrap(m.hostPort))
	if err := os.WriteFile(m.wrapperPath, []byte(content), 0o700); err != nil {
		return fmt.Errorf("write ssh wrapper: %w", err)
	}
	return nil
}

func ensureNoRunningEphemeralLinux() error {
	output, err := exec.Command("docker", "ps", "--filter", "name=^/ephemeral-linux-", "--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("check running containers: %w", err)
	}
	name := strings.TrimSpace(string(output))
	if name != "" {
		return fmt.Errorf("ephemeral-linux currently only works with one running linux container; stop %q first", name)
	}
	return nil
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func (m *Manager) ensureImage() error {
	if err := exec.Command("docker", "image", "inspect", defaultImage).Run(); err == nil {
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "ephemeral-linux-image-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "entrypoint.sh"), []byte(entrypointScript), 0o755); err != nil {
		return fmt.Errorf("write entrypoint: %w", err)
	}

	output, err := exec.Command("docker", "build", "-t", defaultImage, tmpDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("build ssh image: %w\n%s", err, string(output))
	}
	return nil
}

const dockerfile = `FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        openssh-server sudo bash coreutils procps \
        man-db manpages manpages-dev \
        gawk grep sed findutils diffutils less vim-tiny \
        util-linux iproute2 net-tools \
        gcc valgrind make libc6-dev \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /var/run/sshd

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 22
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
`

const entrypointScript = `#!/usr/bin/env bash
set -euo pipefail

user="${EPHEMERAL_USER:-user}"
pass="${EPHEMERAL_PASSWORD:-pass}"

ssh-keygen -A >/dev/null

if ! id "$user" >/dev/null 2>&1; then
  useradd -m -s /bin/bash "$user"
fi

echo "$user:$pass" | chpasswd
usermod -aG sudo "$user" >/dev/null 2>&1 || true

cat >/etc/ssh/sshd_config <<'EOF'
Port 22
PasswordAuthentication yes
PermitRootLogin no
UsePAM yes
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
Subsystem sftp /usr/lib/openssh/sftp-server
EOF

exec /usr/sbin/sshd -D -e
`

func shellWrap(v string) string {
	return strings.ReplaceAll(v, "'", "")
}
