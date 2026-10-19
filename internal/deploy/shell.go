package deploy

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

type Shell struct {
	client  *ssh.Client
	session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
}

func OpenShell(key, address, command string, cols, rows int) (*Shell, error) {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	signer, err := ssh.ParsePrivateKey([]byte(key))
	if err != nil {
		return nil, fmt.Errorf("La clé du serveur est illisible.")
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(address, "22"), &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return checkHostKey(address, key)
		},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("La connexion au serveur a échoué.")
	}
	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("La session n'a pas pu s'ouvrir.")
	}
	if err := session.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("Le terminal n'a pas pu s'ouvrir.")
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		client.Close()
		return nil, fmt.Errorf("Le terminal n'a pas pu s'ouvrir.")
	}
	reader, writer := io.Pipe()
	out := &lockedWriter{w: writer}
	session.Stdout = out
	session.Stderr = out
	shell := &Shell{client: client, session: session, Stdin: stdin, Stdout: reader}
	if command == "" {
		err = session.Shell()
	} else {
		err = session.Start(command)
	}
	if err != nil {
		_ = writer.Close()
		shell.Close()
		return nil, fmt.Errorf("Le terminal n'a pas pu s'ouvrir.")
	}
	go func() {
		_ = session.Wait()
		_ = writer.Close()
	}()
	return shell, nil
}

func (s *Shell) Resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	_ = s.session.WindowChange(rows, cols)
}

func (s *Shell) Close() {
	if s.session != nil {
		_ = s.session.Close()
	}
	if s.client != nil {
		_ = s.client.Close()
	}
}
