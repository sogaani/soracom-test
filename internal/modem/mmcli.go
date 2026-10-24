package modem

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// MMCLI sends SMS/USSD through ModemManager using the mmcli command.
// Running it usually requires root (sudo) or polkit permission.
type MMCLI struct {
	// Modem is the mmcli modem selector, e.g. "any" or "0".
	Modem string
}

var smsPathRe = regexp.MustCompile(`/org/freedesktop/ModemManager1/SMS/(\d+)`)

// SendSMS creates an SMS object in ModemManager and sends it.
func (m *MMCLI) SendSMS(ctx context.Context, number, text string) error {
	// mmcli parses --messaging-create-sms as key='value' pairs, so a single
	// quote in the text cannot be expressed.
	if strings.ContainsRune(text, '\'') {
		return fmt.Errorf("mmcli backend cannot send text containing a single quote; use the at backend")
	}
	out, err := m.run(ctx, "-m", m.modem(), fmt.Sprintf("--messaging-create-sms=text='%s',number='%s'", text, number))
	if err != nil {
		return err
	}
	match := smsPathRe.FindStringSubmatch(out)
	if match == nil {
		return fmt.Errorf("could not find created SMS path in mmcli output: %s", out)
	}
	if _, err := m.run(ctx, "-s", match[1], "--send"); err != nil {
		return err
	}
	// Remove the sent message from the modem's storage. Failure is harmless.
	m.run(ctx, "-m", m.modem(), "--messaging-delete-sms="+match[1]) //nolint:errcheck,gosec // best-effort cleanup
	return nil
}

// SendUSSD initiates a USSD session and returns the network's reply.
func (m *MMCLI) SendUSSD(ctx context.Context, mmi string) (string, error) {
	out, err := m.run(ctx, "-m", m.modem(), "--3gpp-ussd-initiate="+mmi)
	// Close the session whether or not the initiate succeeded so the next
	// request does not fail with "session already active".
	// It fails when the network already closed the session, which is fine.
	m.run(ctx, "-m", m.modem(), "--3gpp-ussd-cancel") //nolint:errcheck,gosec // best-effort cleanup
	if err != nil {
		return "", err
	}
	// Output: USSD session initiated; new reply from network: '...'
	if _, reply, ok := strings.Cut(out, "new reply from network:"); ok {
		return strings.Trim(strings.TrimSpace(reply), "'"), nil
	}
	return strings.TrimSpace(out), nil
}

func (m *MMCLI) modem() string {
	if m.Modem == "" {
		return "any"
	}
	return m.Modem
}

func (m *MMCLI) run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "mmcli", args...).CombinedOutput() //nolint:gosec // fixed binary; args are mmcli options built by this package
	if err != nil {
		return "", fmt.Errorf("mmcli %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
