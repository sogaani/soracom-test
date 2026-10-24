package payload

import (
	"encoding/hex"
	"testing"
)

func TestBinary(t *testing.T) {
	tests := []struct {
		s    Sample
		want string
	}{
		{Sample{CPUTemp: 48.3, Load1: 0.12, UptimeSec: 3600}, "01e3000c00000e10"},
		{Sample{CPUTemp: -5.5, Load1: 0, UptimeSec: 0}, "ffc9000000000000"},
		{Sample{CPUTemp: 99999, Load1: 9999, UptimeSec: -1}, "7fffffff00000000"},
	}
	for _, tt := range tests {
		if got := hex.EncodeToString(tt.s.Binary()); got != tt.want {
			t.Errorf("Binary(%+v) = %s, want %s", tt.s, got, tt.want)
		}
	}
}
