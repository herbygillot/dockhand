package provision

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Vanilla images are built with Remote Login enabled and these
// credentials; they are the published, documented way into one, not a
// secret. dockhand uses them exactly once per image, to install the
// guest agent, and never again.
const (
	guestUser     = "admin"
	guestPassword = "admin"
)

// sshRun runs one script in the guest over SSH and returns its output.
//
// Password authentication only, and nothing else offered. A developer's
// machine has an agent full of keys, and sshd closes the connection on
// MaxAuthTries long before reaching the password — which is a confusing
// way to fail when the password was the only credential that was ever
// going to work.
//
// The host key is not checked. It cannot be: the guest was created
// seconds ago from an image, on a private network, and has never been
// seen before. Pinning something unknowable would be ceremony, and the
// threat it defends against — a machine on the host-only network
// impersonating a VM tart just started — is not one this is placed to
// answer.
func sshRun(ctx context.Context, host, script string) (string, error) {
	client, err := sshClient(ctx, host)
	if err != nil {
		return "", err
	}
	defer client.Close() //nolint:errcheck // best-effort shutdown

	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close() //nolint:errcheck // best-effort shutdown

	var out bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &out
	if err := sess.Run(script); err != nil {
		return out.String(), fmt.Errorf("guest script failed: %w", err)
	}
	return out.String(), nil
}

// sshPush streams one local file into the guest, byte for byte, over
// the same channel the bootstrap uses. It exists for payloads far too
// large to inline in a script — an Xcode archive is tens of gigabytes —
// and a raw pipe into cat is the fastest transfer this package can
// have without growing a dependency.
func sshPush(ctx context.Context, host, local, remote string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only file

	client, err := sshClient(ctx, host)
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck // best-effort shutdown

	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close() //nolint:errcheck // best-effort shutdown

	sess.Stdin = f
	var out bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &out
	if err := sess.Run("cat > " + remote); err != nil {
		return fmt.Errorf("writing %s in the guest: %w: %s", remote, err, strings.TrimSpace(out.String()))
	}
	return nil
}

// sshClient dials the guest with the image's published credentials.
func sshClient(ctx context.Context, host string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            guestUser,
		Auth:            []ssh.AuthMethod{ssh.Password(guestPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // a just-created guest has no known key
		Timeout:         15 * time.Second,
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "22"))
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, host, cfg)
	if err != nil {
		conn.Close() //nolint:errcheck // best-effort on the error path
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// waitSSH waits for the guest to accept a login. It is the only
// readiness signal available before the agent exists.
func waitSSH(ctx context.Context, host string) error {
	for i := 0; i < 120; i++ {
		if _, err := sshRun(ctx, host, "true"); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("guest at %s never accepted an ssh login", host)
}
