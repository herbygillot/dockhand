package provision

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func sshClient(ctx context.Context, host string) (*ssh.Client, error) {
	address := net.JoinHostPort(host, "22")
	config := &ssh.ClientConfig{
		User:            "admin",
		Auth:            []ssh.AuthMethod{ssh.Password("admin")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return ssh.NewClient(clientConnection, channels, requests), nil
}

func sshRun(ctx context.Context, host, script string) (string, error) {
	client, err := sshClient(ctx, host)
	if err != nil {
		return "", err
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var output bytes.Buffer
	session.Stdout, session.Stderr = &output, &output
	if err := session.Run(script); err != nil {
		return output.String(), fmt.Errorf("guest script failed: %w", err)
	}
	return output.String(), nil
}

func waitSSH(ctx context.Context, host string) error {
	var last error
	for attempt := 0; attempt < 120; attempt++ {
		if _, err := sshRun(ctx, host, "/usr/bin/true"); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("guest at %s did not accept its documented admin login: %s", host, strings.TrimSpace(last.Error()))
}
