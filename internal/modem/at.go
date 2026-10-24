package modem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// AT sends SMS/USSD by writing AT commands to the modem's serial port.
//
// ModemManager also opens the AT ports, so stop it (or mark the port as
// ignored) before using this backend.
type AT struct {
	// Device is the AT command port, e.g. /dev/ttyUSB2.
	Device string
	// Timeout bounds each command's wait for a final result.
	Timeout time.Duration
}

// SendSMS sends text with AT+CMGS in text mode.
func (a *AT) SendSMS(ctx context.Context, number, text string) (err error) {
	p, err := a.open()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, p.close()) }()

	for _, cmd := range []string{"ATE0", "AT+CMGF=1", `AT+CSCS="IRA"`} {
		if _, err := p.command(ctx, cmd, a.timeout()); err != nil {
			return err
		}
	}
	if err := p.write(fmt.Sprintf("AT+CMGS=%q\r", number)); err != nil {
		return err
	}
	if err := p.waitPrompt(ctx, a.timeout()); err != nil {
		return err
	}
	// Body is terminated with Ctrl+Z. Network submission can take a while.
	if err := p.write(text + "\x1a"); err != nil {
		return err
	}
	_, err = p.waitFinal(ctx, 2*a.timeout(), "")
	return err
}

// SendUSSD sends AT+CUSD with DCS 15 and waits for the +CUSD reply.
func (a *AT) SendUSSD(ctx context.Context, mmi string) (reply string, err error) {
	p, err := a.open()
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, p.close()) }()

	for _, cmd := range []string{"ATE0", `AT+CSCS="GSM"`} {
		if _, err := p.command(ctx, cmd, a.timeout()); err != nil {
			return "", err
		}
	}
	if err := p.write(fmt.Sprintf("AT+CUSD=1,%q,15\r", mmi)); err != nil {
		return "", err
	}
	lines, err := p.waitFinal(ctx, 2*a.timeout(), "+CUSD:")
	if err != nil {
		return "", err
	}
	// +CUSD: <m>,"<str>",<dcs>
	for _, l := range lines {
		if rest, ok := strings.CutPrefix(l, "+CUSD:"); ok {
			if i, j := strings.Index(rest, `"`), strings.LastIndex(rest, `"`); i >= 0 && j > i {
				return rest[i+1 : j], nil
			}
			return strings.TrimSpace(rest), nil
		}
	}
	return "", nil
}

func (a *AT) timeout() time.Duration {
	if a.Timeout <= 0 {
		return 10 * time.Second
	}
	return a.Timeout
}

type port struct {
	f     *os.File
	lines chan string
	errc  chan error
}

func (a *AT) open() (*port, error) {
	if a.Device == "" {
		return nil, fmt.Errorf("AT device is not set")
	}
	// Put the tty in raw mode so the line discipline does not echo or
	// translate characters. Linux stty uses -F.
	if out, err := exec.Command("stty", "-F", a.Device, "115200", "raw", "-echo").CombinedOutput(); err != nil { //nolint:gosec // the device path is the intended input
		return nil, fmt.Errorf("stty %s: %w: %s", a.Device, err, strings.TrimSpace(string(out)))
	}
	f, err := os.OpenFile(a.Device, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	p := &port{f: f, lines: make(chan string, 32), errc: make(chan error, 1)}
	go p.readLoop()
	return p, nil
}

func (p *port) close() error { return p.f.Close() }

// readLoop splits modem output into lines. The SMS prompt "> " has no line
// terminator, so it is emitted on its own as soon as it is seen.
func (p *port) readLoop() {
	r := bufio.NewReader(p.f)
	var cur strings.Builder
	for {
		b, err := r.ReadByte()
		if err != nil {
			p.errc <- err
			return
		}
		switch b {
		case '\r', '\n':
			if cur.Len() > 0 {
				p.lines <- cur.String()
				cur.Reset()
			}
		default:
			cur.WriteByte(b)
			if cur.String() == "> " {
				p.lines <- ">"
				cur.Reset()
			}
		}
	}
}

func (p *port) write(s string) error {
	_, err := p.f.WriteString(s)
	return err
}

func (p *port) command(ctx context.Context, cmd string, timeout time.Duration) ([]string, error) {
	if err := p.write(cmd + "\r"); err != nil {
		return nil, err
	}
	return p.waitFinal(ctx, timeout, "")
}

// waitFinal collects lines until OK or an error result. If want is set, it
// also keeps waiting after OK until a line with that prefix arrives (used for
// the unsolicited +CUSD reply).
func (p *port) waitFinal(ctx context.Context, timeout time.Duration, want string) ([]string, error) {
	deadline := time.After(timeout)
	var lines []string
	gotOK, gotWant := false, want == ""
	for {
		select {
		case l := <-p.lines:
			lines = append(lines, l)
			switch {
			case l == "OK":
				gotOK = true
			case l == "ERROR", strings.HasPrefix(l, "+CME ERROR"), strings.HasPrefix(l, "+CMS ERROR"):
				return lines, fmt.Errorf("modem returned %s", l)
			case want != "" && strings.HasPrefix(l, want):
				gotWant = true
			}
			if gotOK && gotWant {
				return lines, nil
			}
		case err := <-p.errc:
			return lines, fmt.Errorf("reading modem: %w", err)
		case <-deadline:
			return lines, fmt.Errorf("timed out waiting for modem response (got %q)", lines)
		case <-ctx.Done():
			return lines, ctx.Err()
		}
	}
}

func (p *port) waitPrompt(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case l := <-p.lines:
			if l == ">" {
				return nil
			}
			if l == "ERROR" || strings.Contains(l, "ERROR") {
				return fmt.Errorf("modem returned %s", l)
			}
		case err := <-p.errc:
			return fmt.Errorf("reading modem: %w", err)
		case <-deadline:
			return fmt.Errorf("timed out waiting for SMS prompt")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
