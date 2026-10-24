// Package payload builds sample data from the Raspberry Pi's own sensors so
// the demos have something meaningful to send without extra hardware.
package payload

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sample is one reading of the device's state.
type Sample struct {
	CPUTemp   float64 `json:"cpuTemp"` // °C; 0 if unavailable
	Load1     float64 `json:"load1"`
	UptimeSec int64   `json:"uptime"`
	Time      int64   `json:"time"` // UNIX ms; Harvest Data can use it as the timestamp
}

// Read collects a sample from /sys and /proc. Values that cannot be read
// (e.g. when running on a non-Linux machine) are left as zero.
func Read() Sample {
	s := Sample{Time: time.Now().UnixMilli()}
	if v, err := readFloat("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		s.CPUTemp = round(v/1000, 1)
	}
	if v, err := readFloat("/proc/loadavg"); err == nil {
		s.Load1 = v
	}
	if v, err := readFloat("/proc/uptime"); err == nil {
		s.UptimeSec = int64(v)
	}
	return s
}

// JSON encodes the sample as JSON.
func (s Sample) JSON() []byte {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err) // unreachable: Sample has only numeric fields
	}
	return b
}

// Text encodes the sample as short GSM 7-bit safe text, suitable for SMS and
// USSD, using the same keys as JSON, e.g. "cpuTemp=48.3,load1=0.12,uptime=3600".
func (s Sample) Text() string {
	return fmt.Sprintf("cpuTemp=%.1f,load1=%.2f,uptime=%d", s.CPUTemp, s.Load1, s.UptimeSec)
}

// Binary encodes the sample as 8 bytes, big endian:
//
//	int16 cpuTemp*10 | uint16 load1*100 | uint32 uptime
//
// A matching SORACOM Binary Parser format is:
//
//	cpuTemp:0:int:16:/10 load1:2:uint:16:/100 uptime:4:uint:32
func (s Sample) Binary() []byte {
	b := make([]byte, 0, 8)
	b = binary.BigEndian.AppendUint16(b, uint16(int16(clamp(math.Round(s.CPUTemp*10), math.MinInt16, math.MaxInt16)))) //nolint:gosec // intended two's complement
	b = binary.BigEndian.AppendUint16(b, uint16(clamp(math.Round(s.Load1*100), 0, math.MaxUint16)))
	return binary.BigEndian.AppendUint32(b, uint32(clamp(float64(s.UptimeSec), 0, math.MaxUint32)))
}

// clamp limits v to [lo, hi] so the integer conversion cannot overflow.
func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// readFloat reads the first whitespace-separated field of a file as a float.
func readFloat(path string) (float64, error) {
	b, err := os.ReadFile(path) //nolint:gosec // callers pass fixed /sys and /proc paths
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, fmt.Errorf("%s is empty", path)
	}
	return strconv.ParseFloat(fields[0], 64)
}

func round(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}
