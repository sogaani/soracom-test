// Command client runs on the Raspberry Pi (with a SORACOM Air SIM) and sends
// data to the SORACOM Unified Endpoint using each entry point available to
// Air for Cellular: HTTP, TCP, UDP, SMS and USSD.
//
// Usage:
//
//	client <method> [flags]
//
// Run "client <method> -h" for the flags of each method.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/sogaani/soracom-test/internal/modem"
	"github.com/sogaani/soracom-test/internal/payload"
	"github.com/sogaani/soracom-test/internal/unified"
)

const usage = `Send data to the SORACOM Unified Endpoint.

Usage:
  client <method> [flags]

Methods:
  http   POST to http://uni.soracom.io (port 80 or 8888)
  tcp    send to uni.soracom.io:23080/tcp (optionally SORACOM Binary Format v1)
  udp    send to uni.soracom.io:23080/udp
  sms    send an SMS to 901001 via the modem
  ussd   send USSD *901001*<text># via the modem
  all    send the same sample once over http, tcp and udp

With no -data/-hex flag, a sample of the Pi's CPU temperature, load and
uptime is sent. Run "client <method> -h" for details.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	method, args := os.Args[1], os.Args[2:]
	var err error
	switch method {
	case "http":
		err = runHTTP(ctx, args)
	case "tcp":
		err = runTCP(ctx, args)
	case "udp":
		err = runUDP(ctx, args)
	case "sms":
		err = runSMS(ctx, args)
	case "ussd":
		err = runUSSD(ctx, args)
	case "all":
		err = runAll(ctx, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown method %q\n\n%s", method, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// commonFlags are shared by the IP based methods.
type commonFlags struct {
	host    string
	data    string
	hexData string
	timeout time.Duration
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&c.host, "host", unified.DefaultHost, "Unified Endpoint host (uni.soracom.io or unified.soracom.io)")
	fs.StringVar(&c.data, "data", "", "data to send as a string (default: Pi sample)")
	fs.StringVar(&c.hexData, "hex", "", "data to send as hex, e.g. 0102030405 (overrides -data)")
	fs.DurationVar(&c.timeout, "timeout", 10*time.Second, "time to wait for the response")
}

// body returns the user supplied data, or def if none was given.
func (c *commonFlags) body(def []byte) ([]byte, error) {
	switch {
	case c.hexData != "":
		return hex.DecodeString(strings.ReplaceAll(c.hexData, " ", ""))
	case c.data != "":
		return []byte(c.data), nil
	default:
		return def, nil
	}
}

func runHTTP(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	var c commonFlags
	c.register(fs)
	port := fs.Int("port", unified.DefaultHTTPPort, "HTTP port (80 or 8888)")
	contentType := fs.String("content-type", "", "Content-Type (default: application/json for the sample/-data, application/octet-stream for -hex)")
	timestamp := fs.Bool("timestamp", false, "send x-soracom-timestamp with the current time (Harvest Data stores it as the record time)")
	count := fs.Int("count", 1, "number of requests to send")
	interval := fs.Duration("interval", 5*time.Second, "interval between requests when -count > 1")
	closeConn := fs.Bool("close", false, "send 'Connection: close' (disable keep-alive)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ct := *contentType
	if ct == "" {
		ct = "application/json"
		if c.hexData != "" {
			ct = "application/octet-stream"
		}
	}

	client := unified.NewHTTPClient(c.host, *port, c.timeout)
	client.CloseConnection = *closeConn
	for i := 0; i < *count; i++ {
		if i > 0 {
			select {
			case <-time.After(*interval):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		body, err := c.body(payload.Read().JSON())
		if err != nil {
			return err
		}
		req := unified.HTTPRequest{ContentType: ct, Body: body}
		if *timestamp {
			req.Timestamp = time.Now()
		}
		fmt.Printf("[http] POST http://%s:%d/ (%s) %s\n", c.host, *port, ct, display(body))
		resp, err := client.Send(ctx, req)
		if err != nil {
			return err
		}
		fmt.Print(resp)
	}
	return nil
}

func runTCP(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("tcp", flag.ExitOnError)
	var c commonFlags
	c.register(fs)
	port := fs.Int("port", unified.DefaultSocketPort, "TCP port")
	binaryFormat := fs.Bool("binary-format", false, "wrap data in a SORACOM Binary Format v1 frame (enable it for the TCP entry point in the group settings)")
	binarySample := fs.Bool("binary", false, "send the Pi sample in its 8 byte binary encoding (for the Binary Parser)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	def := payload.Read().JSON()
	if *binarySample {
		def = payload.Read().Binary()
	}
	body, err := c.body(def)
	if err != nil {
		return err
	}
	mode := "raw"
	if *binaryFormat {
		mode = "binary format v1"
	}
	fmt.Printf("[tcp] %s:%d (%s) %s\n", c.host, *port, mode, display(body))
	resp, err := unified.SendTCP(ctx, c.host, *port, body, *binaryFormat, c.timeout)
	if err != nil {
		return err
	}
	fmt.Print(resp)
	return nil
}

func runUDP(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("udp", flag.ExitOnError)
	var c commonFlags
	c.register(fs)
	port := fs.Int("port", unified.DefaultSocketPort, "UDP port")
	binarySample := fs.Bool("binary", false, "send the Pi sample in its 8 byte binary encoding (for the Binary Parser)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	def := payload.Read().JSON()
	if *binarySample {
		def = payload.Read().Binary()
	}
	body, err := c.body(def)
	if err != nil {
		return err
	}
	fmt.Printf("[udp] %s:%d %s\n", c.host, *port, display(body))
	resp, err := unified.SendUDP(ctx, c.host, *port, body, c.timeout)
	if err != nil {
		return err
	}
	fmt.Print(resp)
	return nil
}

// modemFlags are shared by sms and ussd.
type modemFlags struct {
	backend string
	mmModem string
	device  string
	data    string
	code    string
	timeout time.Duration
}

func (m *modemFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&m.backend, "backend", "mmcli", "modem backend: mmcli (ModemManager) or at (raw AT commands on -device)")
	fs.StringVar(&m.mmModem, "modem", "any", "mmcli modem selector (mmcli backend)")
	fs.StringVar(&m.device, "device", "/dev/ttyUSB2", "AT command port (at backend)")
	fs.StringVar(&m.data, "data", "", "text to send (default: Pi sample as short text)")
	fs.StringVar(&m.code, "code", modem.CodeUnifiedEndpoint, "service code / number (901001 = Unified Endpoint)")
	fs.DurationVar(&m.timeout, "timeout", 30*time.Second, "time to wait for the modem")
}

func (m *modemFlags) sender() (modem.Sender, error) {
	switch m.backend {
	case "mmcli":
		return &modem.MMCLI{Modem: m.mmModem}, nil
	case "at":
		return &modem.AT{Device: m.device, Timeout: m.timeout}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q", m.backend)
	}
}

func (m *modemFlags) text() string {
	if m.data != "" {
		return m.data
	}
	return payload.Read().Text()
}

func runSMS(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sms", flag.ExitOnError)
	var m modemFlags
	m.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	text := m.text()
	if err := modem.ValidateSMS(text); err != nil {
		return err
	}
	s, err := m.sender()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	fmt.Printf("[sms] to %s via %s: %s\n", m.code, m.backend, text)
	if err := s.SendSMS(ctx, m.code, text); err != nil {
		return err
	}
	// SMS has no response channel; check the result in Harvest Data etc.
	fmt.Println("sent")
	return nil
}

func runUSSD(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ussd", flag.ExitOnError)
	var m modemFlags
	m.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	mmi, err := modem.USSDString(m.code, m.text())
	if err != nil {
		return err
	}
	s, err := m.sender()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	fmt.Printf("[ussd] %s via %s\n", mmi, m.backend)
	reply, err := s.SendUSSD(ctx, mmi)
	if err != nil {
		return err
	}
	fmt.Printf("reply: %s\n", reply)
	return nil
}

func runAll(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("all", flag.ExitOnError)
	host := fs.String("host", unified.DefaultHost, "Unified Endpoint host")
	timeout := fs.Duration("timeout", 10*time.Second, "time to wait for each response")
	if err := fs.Parse(args); err != nil {
		return err
	}

	body := payload.Read().JSON()
	var failed []string
	steps := []struct {
		name string
		send func() (*unified.Response, error)
	}{
		{"http", func() (*unified.Response, error) {
			return unified.NewHTTPClient(*host, unified.DefaultHTTPPort, *timeout).
				Send(ctx, unified.HTTPRequest{ContentType: "application/json", Body: body})
		}},
		{"tcp", func() (*unified.Response, error) {
			return unified.SendTCP(ctx, *host, unified.DefaultSocketPort, body, false, *timeout)
		}},
		{"udp", func() (*unified.Response, error) {
			return unified.SendUDP(ctx, *host, unified.DefaultSocketPort, body, *timeout)
		}},
	}
	for _, s := range steps {
		fmt.Printf("[%s] %s\n", s.name, body)
		resp, err := s.send()
		if err != nil {
			fmt.Printf("error: %v\n", err)
			failed = append(failed, s.name)
			continue
		}
		fmt.Print(resp)
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

// display renders data as text if printable, otherwise as hex.
func display(b []byte) string {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return "0x" + hex.EncodeToString(b)
		}
	}
	return string(b)
}
