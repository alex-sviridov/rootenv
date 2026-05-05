package ssh

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

type serverConnection struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
}

// parseConnection unmarshals the connection JSON from the server record.
func parseConnection(raw json.RawMessage) (*serverConnection, error) {
	var c serverConnection
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse connection: %w", err)
	}
	if c.Host == "" {
		return nil, fmt.Errorf("connection: missing host")
	}
	if c.User == "" {
		return nil, fmt.Errorf("connection: missing user")
	}
	if c.Port == 0 {
		c.Port = 22
	}
	return &c, nil
}

// loadSigner reads the private key at path and returns a gossh.Signer.
func loadSigner(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", path, err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse private key %q: %w", path, err)
	}
	return signer, nil
}

// dialSSH establishes an SSH client connection using public-key auth.
func dialSSH(conn *serverConnection, signer gossh.Signer, m *SSHMetrics) (*gossh.Client, error) {
	cfg := &gossh.ClientConfig{
		User: conn.User,
		Auth: []gossh.AuthMethod{gossh.PublicKeys(signer)},
		// HostKeyCallback is InsecureIgnoreHostKey because lab VMs are ephemeral
		// and their host keys are not pre-registered. Network-level isolation
		// (VPC / internal Docker network) is the trust boundary here.
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
	}
	addr := net.JoinHostPort(conn.Host, fmt.Sprintf("%d", conn.Port))
	start := time.Now()
	client, err := gossh.Dial("tcp", addr, cfg)
	if err != nil {
		if m != nil {
			m.sshDialErrors.Inc()
		}
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	if m != nil {
		m.sshDialDuration.Observe(time.Since(start).Seconds())
	}
	return client, nil
}

// openShellWithPipes opens a new SSH session with a PTY, gets stdin/stdout pipes,
// and starts the interactive shell. Pipes must be obtained before Shell() is called.
// Caller is responsible for closing the session and client.
func openShellWithPipes(client *gossh.Client, m *SSHMetrics) (*gossh.Session, io.WriteCloser, io.Reader, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("new session: %w", err)
	}

	modes := gossh.TerminalModes{
		gossh.ECHO:          1,
		gossh.TTY_OP_ISPEED: 14400,
		gossh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		session.Close()
		return nil, nil, nil, fmt.Errorf("request pty: %w", err)
	}

	// Get pipes before calling Shell().
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		session.Close()
		return nil, nil, nil, fmt.Errorf("start shell: %w", err)
	}
	if m != nil {
		m.sshShellsStarted.Inc()
	}
	return session, stdin, stdout, nil
}
