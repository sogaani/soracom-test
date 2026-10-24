package modem

import (
	"strings"
	"testing"
)

func TestUSSDString(t *testing.T) {
	got, err := USSDString(CodeUnifiedEndpoint, "temp=21.5,hum=40")
	if err != nil {
		t.Fatal(err)
	}
	if want := "*901001*temp=21.5,hum=40#"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	for _, bad := range []string{"", "a*b", "a#b", `{"t":1}`, "日本語", strings.Repeat("a", MaxUSSDLength)} {
		if _, err := USSDString(CodeUnifiedEndpoint, bad); err == nil {
			t.Errorf("USSDString(%q) succeeded, want error", bad)
		}
	}
}

func TestValidateSMS(t *testing.T) {
	if err := ValidateSMS(`{"temperature":20}`); err != nil {
		t.Error(err)
	}
	if err := ValidateSMS(strings.Repeat("a", MaxSMSPayload+1)); err == nil {
		t.Error("expected error for oversized sms")
	}
}
