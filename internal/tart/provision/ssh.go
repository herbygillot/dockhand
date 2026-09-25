package provision

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
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
	return dialSSH(ctx, address, config)
}

func dialSSH(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	connection, err := (&net.Dialer{Timeout: config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	if config.Timeout > 0 {
		_ = connection.SetDeadline(time.Now().Add(config.Timeout))
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err != nil {
		_ = connection.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	_ = connection.SetDeadline(time.Time{})
	return ssh.NewClient(clientConnection, channels, requests), nil
}

func sshRun(ctx context.Context, host, script string) (string, error) {
	client, err := sshClient(ctx, host)
	if err != nil {
		return "", err
	}
	defer client.Close()
	stop := closeSSHOnCancellation(ctx, client)
	defer stop()
	session, err := client.NewSession()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}
	defer session.Close()
	output, err := session.CombinedOutput(script)
	if err != nil {
		if ctx.Err() != nil {
			return string(output), ctx.Err()
		}
		return string(output), fmt.Errorf("guest script failed: %w", err)
	}
	return string(output), nil
}

func sshPush(ctx context.Context, host, local, remote string) error {
	if remote != "/private/tmp/Xcode.xip" {
		return fmt.Errorf("unsupported guest upload path %q", remote)
	}
	file, err := os.Open(local)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", local)
	}
	client, err := sshClient(ctx, host)
	if err != nil {
		return err
	}
	defer client.Close()
	stop := closeSSHOnCancellation(ctx, client)
	defer stop()
	session, err := client.NewSession()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer session.Close()
	session.Stdin = file
	session.Stdout = io.Discard
	var output bytes.Buffer
	session.Stderr = &output
	if err := session.Run("/bin/cat > /private/tmp/Xcode.xip"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("writing Xcode archive in the guest: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return nil
}

func closeSSHOnCancellation(ctx context.Context, client *ssh.Client) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func waitSSH(parent context.Context, host string) error {
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	var last error
	for attempt := 0; attempt < 120; attempt++ {
		if _, err := sshRun(ctx, host, "/usr/bin/true"); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			// A dial macOS blocks for want of Local Network permission for
			// the app that launched dockhand fails the same way, which is
			// why setup is moving to /usr/bin/ssh (roadmap step 2).
			return fmt.Errorf("guest at %s did not accept SSH within 4 minutes: %w; last attempt: %v; if the guest is up, the app running dockhand may lack macOS's Local Network permission", host, ctx.Err(), last)
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("guest at %s did not accept its documented admin login: %s", host, strings.TrimSpace(last.Error()))
}
