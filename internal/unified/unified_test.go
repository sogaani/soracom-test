package unified

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEncodeBinaryFormatV1(t *testing.T) {
	// Example from https://users.soracom.io/ja-jp/docs/binary-format/
	got, err := EncodeBinaryFormatV1([]byte{1, 2, 3, 4, 5, 6})
	if err != nil {
		t.Fatal(err)
	}
	if want := "00060102030405064917"; hex.EncodeToString(got) != want {
		t.Errorf("got %x, want %s", got, want)
	}

	if _, err := EncodeBinaryFormatV1(nil); err == nil {
		t.Error("expected error for empty body")
	}
	if _, err := EncodeBinaryFormatV1(make([]byte, MaxBinaryFormatV1Body+1)); err == nil {
		t.Error("expected error for oversized body")
	}
}

func TestParseSocketResponse(t *testing.T) {
	tests := []struct {
		in       string
		wantCode int
		wantBody string
	}{
		{"201", 201, ""},
		{"200 Hi", 200, "Hi"},
		{`200 "Hi"`, 200, `"Hi"`},
		{`{"result":"ok"}`, 0, `{"result":"ok"}`},
	}
	for _, tt := range tests {
		r := parseSocketResponse([]byte(tt.in))
		if r.StatusCode != tt.wantCode || string(r.Body) != tt.wantBody {
			t.Errorf("parse(%q) = %d %q, want %d %q", tt.in, r.StatusCode, r.Body, tt.wantCode, tt.wantBody)
		}
	}
}

func TestUnifiedBody(t *testing.T) {
	r := &Response{StatusCode: 207, Body: []byte(`{"result":"ng","detail":{
		"SoracomFunk":{"statusCode":200,"body":"SGk=","encoding":"base64"},
		"SoracomHarvest":{"statusCode":400}}}`)}
	u, ok := r.Unified()
	if !ok {
		t.Fatal("expected unified body")
	}
	if u.Result != "ng" || u.Detail["SoracomFunk"].DecodedBody() != "Hi" || u.Detail["SoracomHarvest"].StatusCode != http.StatusBadRequest {
		t.Errorf("unexpected %+v", u)
	}
	if _, ok := (&Response{Body: []byte("Hi")}).Unified(); ok {
		t.Error("plain body parsed as unified")
	}
}

func TestHTTPClientSend(t *testing.T) {
	var gotCT, gotTS, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotTS = r.Header.Get("x-soracom-timestamp")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	host, port := splitHostPort(t, srv.Listener.Addr().String())
	c := NewHTTPClient(host, port, 5*time.Second)
	ts := time.UnixMilli(1671883114123)
	resp, err := c.Send(context.Background(), HTTPRequest{ContentType: "application/json", Body: []byte(`{"a":1}`), Timestamp: ts})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated || gotCT != "application/json" || gotTS != "1671883114123" || gotBody != `{"a":1}` {
		t.Errorf("status=%d ct=%q ts=%q body=%q", resp.StatusCode, gotCT, gotTS, gotBody)
	}
}

func TestSendTCPBinaryFormat(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	received := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		received <- buf[:n]
		conn.Write([]byte("201"))
		// Keep the connection open like the real endpoint may.
		time.Sleep(time.Second)
	}()

	host, port := splitHostPort(t, ln.Addr().String())
	resp, err := SendTCP(context.Background(), host, port, []byte{1, 2, 3, 4, 5, 6}, true, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if got := <-received; hex.EncodeToString(got) != "00060102030405064917" {
		t.Errorf("server received %x", got)
	}
}

func TestSendUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 1024)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		pc.WriteTo(append([]byte("200 "), bytes.ToUpper(buf[:n])...), addr)
	}()

	host, port := splitHostPort(t, pc.LocalAddr().String())
	resp, err := SendUDP(context.Background(), host, port, []byte("hello"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "HELLO" {
		t.Errorf("got %d %q", resp.StatusCode, resp.Body)
	}
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	addr = strings.TrimPrefix(addr, "http://")
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return h, n
}
