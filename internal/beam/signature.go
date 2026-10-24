// Package beam receives data forwarded by SORACOM Beam and turns it into
// store.Message values.
package beam

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Headers added by Beam. IMSI, IMEI, MSISDN and SIM ID are each optional and
// toggled in the Beam entry point settings.
const (
	HeaderIMSI             = "X-Soracom-Imsi"
	HeaderIMEI             = "X-Soracom-Imei"
	HeaderMSISDN           = "X-Soracom-Msisdn"
	HeaderSIMID            = "X-Soracom-Sim-Id"
	HeaderTimestamp        = "X-Soracom-Timestamp"
	HeaderSignature        = "X-Soracom-Signature"
	HeaderSignatureVersion = "X-Soracom-Signature-Version"
)

// signedHeaders lists the headers covered by the signature, in signing order.
// https://users.soracom.io/ja-jp/docs/beam/verify-signature/
var signedHeaders = []string{HeaderIMEI, HeaderIMSI, HeaderMSISDN, HeaderSIMID, HeaderTimestamp}

var (
	ErrNoSignature      = errors.New("missing x-soracom-signature")
	ErrBadSignature     = errors.New("signature mismatch")
	ErrBadTimestamp     = errors.New("missing or invalid x-soracom-timestamp")
	ErrStaleTimestamp   = errors.New("x-soracom-timestamp outside the allowed window")
	ErrSignatureVersion = errors.New("unsupported x-soracom-signature-version")
)

// Verifier checks the x-soracom-signature header Beam adds when "署名ヘッダ"
// is enabled with a pre-shared key:
//
//	sha256hex(psk + "x-soracom-imei=..." + "x-soracom-imsi=..." + "x-soracom-msisdn=..."
//	          + "x-soracom-sim-id=..." + "x-soracom-timestamp=...")
//
// where only the headers Beam actually sent are included (the timestamp
// always is). The body, path and Content-Type are not signed, so MaxSkew is
// the only protection against replaying captured headers with another body.
type Verifier struct {
	Key []byte
	// MaxSkew bounds |now - x-soracom-timestamp| to limit replays. Zero
	// disables the check.
	MaxSkew time.Duration
	Now     func() time.Time
}

// Verify returns nil if the request carries a valid signature.
func (v *Verifier) Verify(h http.Header) error {
	sig := h.Get(HeaderSignature)
	if sig == "" {
		return ErrNoSignature
	}
	if ver := h.Get(HeaderSignatureVersion); ver != "" && ver != "20151001" {
		return ErrSignatureVersion
	}
	ts, err := Timestamp(h)
	if err != nil {
		return err
	}
	if v.MaxSkew > 0 {
		now := time.Now
		if v.Now != nil {
			now = v.Now
		}
		if d := now().Sub(ts); d > v.MaxSkew || d < -v.MaxSkew {
			return ErrStaleTimestamp
		}
	}
	want := Sign(v.Key, h)
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(sig)), []byte(want)) != 1 {
		return ErrBadSignature
	}
	return nil
}

// Sign computes the signature Beam would send for the given headers.
func Sign(key []byte, h http.Header) string {
	sum := sha256.New()
	sum.Write(key)
	for _, name := range signedHeaders {
		if v := h.Get(name); v != "" {
			sum.Write([]byte(strings.ToLower(name) + "=" + v))
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Timestamp parses x-soracom-timestamp (UNIX milliseconds).
func Timestamp(h http.Header) (time.Time, error) {
	ms, err := strconv.ParseInt(h.Get(HeaderTimestamp), 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}, ErrBadTimestamp
	}
	return time.UnixMilli(ms), nil
}
