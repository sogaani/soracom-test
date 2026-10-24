// Package modem sends SMS and USSD from the cellular modem to SORACOM.
//
// Two backends are provided:
//   - MMCLI drives ModemManager via the mmcli command (the usual setup on a
//     Raspberry Pi where NetworkManager/ModemManager manage the Air SIM).
//   - AT talks to the modem's AT command port directly (e.g. /dev/ttyUSB2).
//     The port must not be in use by ModemManager.
//
// https://users.soracom.io/ja-jp/docs/air/send-sms/
// https://users.soracom.io/ja-jp/docs/air/send-ussd/
package modem

import (
	"context"
	"fmt"
)

// Service codes. SMS uses them as the destination number; USSD uses them in
// the MMI string "*{code}*{text}#".
const (
	CodeUnifiedEndpoint = "901001"
	CodeBeam            = "901011"
	CodeFunnel          = "901021"
	CodeHarvestData     = "901031"
	CodeFunk            = "901041"
)

const (
	// MaxSMSPayload is the maximum SMS payload SORACOM accepts.
	MaxSMSPayload = 140
	// MaxUSSDLength is the maximum USSD string length in GSM 7-bit
	// characters, including "*{code}*" and "#".
	MaxUSSDLength = 182
)

// Sender sends SMS and USSD messages.
type Sender interface {
	// SendSMS sends text to number.
	SendSMS(ctx context.Context, number, text string) error
	// SendUSSD sends the MMI string and returns the network's reply, if any.
	// The reply carries the destination's response only when it returned
	// Content-Type: text/plain with GSM 7-bit characters.
	SendUSSD(ctx context.Context, mmi string) (string, error)
}

// ValidateSMS checks the SMS payload limits.
func ValidateSMS(text string) error {
	if text == "" {
		return fmt.Errorf("sms text is empty")
	}
	if len(text) > MaxSMSPayload {
		return fmt.Errorf("sms text is %d bytes, max %d", len(text), MaxSMSPayload)
	}
	return nil
}

// USSDString builds and validates the MMI string "*{code}*{text}#".
// USSD only carries the GSM 7-bit basic character set; '*' and '#' are
// rejected because they delimit the MMI string.
func USSDString(code, text string) (string, error) {
	if text == "" {
		return "", fmt.Errorf("ussd text is empty")
	}
	for _, r := range text {
		if r == '*' || r == '#' {
			return "", fmt.Errorf("ussd text must not contain %q", r)
		}
		if !isGSM7Basic(r) {
			return "", fmt.Errorf("ussd text contains %q, which is not in the GSM 7-bit basic character set", r)
		}
	}
	s := "*" + code + "*" + text + "#"
	if n := len([]rune(s)); n > MaxUSSDLength {
		return "", fmt.Errorf("ussd string is %d characters, max %d", n, MaxUSSDLength)
	}
	return s, nil
}

// gsm7Basic is the GSM 03.38 basic character set (without the escape to the
// extension table, so no {}[]~\^|€).
const gsm7Basic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?" +
	"¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

func isGSM7Basic(r rune) bool {
	for _, c := range gsm7Basic {
		if c == r {
			return true
		}
	}
	return false
}
