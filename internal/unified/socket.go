package unified

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

// SendUDP sends one datagram to the UDP entry point and waits for the
// response datagram. The payload is processed as binary data.
func SendUDP(ctx context.Context, host string, port int, payload []byte, timeout time.Duration) (_ *Response, err error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	if _, err := conn.Write(payload); err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	buf := make([]byte, 64*1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("waiting for response: %w", err)
	}
	return parseSocketResponse(buf[:n]), nil
}

// SendTCP sends the payload to the TCP entry point and waits for the
// response. The payload is processed as binary data.
//
// If binaryFormat is true the payload is wrapped in a SORACOM Binary Format v1
// frame; the entry point must have Binary Format v1 enabled in the group
// settings, otherwise the frame bytes are forwarded as-is.
//
// The connection is kept open until the response arrives: closing early can
// cause the data to be discarded.
func SendTCP(ctx context.Context, host string, port int, payload []byte, binaryFormat bool, timeout time.Duration) (_ *Response, err error) {
	if binaryFormat {
		if payload, err = EncodeBinaryFormatV1(payload); err != nil {
			return nil, err
		}
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp4", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	if _, err := conn.Write(payload); err != nil {
		return nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}

	// The endpoint may keep the connection open after responding, so read
	// until EOF or until the stream goes quiet briefly after the first bytes.
	var resp []byte
	buf := make([]byte, 64*1024)
	for {
		n, err := conn.Read(buf)
		resp = append(resp, buf[:n]...)
		if err != nil {
			if len(resp) > 0 && (errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, io.EOF)) {
				break
			}
			return nil, fmt.Errorf("waiting for response: %w", err)
		}
		if len(resp) > 0 {
			if err := conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
				return nil, err
			}
		}
	}
	return parseSocketResponse(resp), nil
}
