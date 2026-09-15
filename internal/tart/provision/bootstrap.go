package provision

import (
	"context"
	"fmt"
	"strings"
)

func (n *native) BootstrapAgent(ctx context.Context, name string) error {
	output, err := n.command(ctx, nil, false, "ip", name, "--wait", "300")
	if err != nil {
		return err
	}
	host := strings.TrimSpace(string(output))
	if host == "" {
		return fmt.Errorf("tart: VM %s has no IP address", name)
	}
	if err := waitSSH(ctx, host); err != nil {
		return err
	}
	outputText, err := sshRun(ctx, host, agentInstallScript())
	if err != nil {
		return fmt.Errorf("tart: installing guest agent: %w: %s", err, strings.TrimSpace(outputText))
	}
	return nil
}
