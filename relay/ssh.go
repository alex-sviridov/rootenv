package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
)

type sshConn struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   interface{ Write([]byte) (int, error) }
	stdout  interface{ Read([]byte) (int, error) }
}

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

// loadSigner reads the private key at path and returns an ssh.Signer.
func loadSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse private key %q: %w", path, err)
	}
	return signer, nil
}

// dialSSH establishes an SSH client connection using public-key auth.
func dialSSH(conn *serverConnection, signer ssh.Signer) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: conn.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// HostKeyCallback is InsecureIgnoreHostKey because lab VMs are ephemeral
		// and their host keys are not pre-registered. Network-level isolation
		// (VPC / internal Docker network) is the trust boundary here.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	}
	addr := net.JoinHostPort(conn.Host, fmt.Sprintf("%d", conn.Port))
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return client, nil
}

// openShell opens a new SSH session with a PTY and interactive shell.
// Caller is responsible for closing both session and client.
func openShell(client *ssh.Client) (*ssh.Session, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	return session, nil
}
