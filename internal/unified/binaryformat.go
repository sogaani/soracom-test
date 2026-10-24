package unified

import (
	"encoding/binary"
	"fmt"
)

// MaxBinaryFormatV1Body is the largest body a Binary Format v1 frame can carry.
const MaxBinaryFormatV1Body = 0xffff

// EncodeBinaryFormatV1 wraps body in a SORACOM Binary Format v1 frame:
//
//	| length (2 bytes, big endian) | body (length bytes) | CRC-16 (2 bytes) |
//
// The checksum is CRC-16/CCITT-FALSE over length+body. The frame lets one
// message span several TCP segments; SORACOM reassembles it (each segment
// must arrive within ~10s of the previous one) and strips the framing before
// forwarding.
// https://users.soracom.io/ja-jp/docs/binary-format/
func EncodeBinaryFormatV1(body []byte) ([]byte, error) {
	if len(body) == 0 || len(body) > MaxBinaryFormatV1Body {
		return nil, fmt.Errorf("binary format v1 body must be 1..%d bytes, got %d", MaxBinaryFormatV1Body, len(body))
	}
	frame := make([]byte, 2, 2+len(body)+2)
	binary.BigEndian.PutUint16(frame, uint16(len(body))) //nolint:gosec // bounds checked above
	frame = append(frame, body...)
	return binary.BigEndian.AppendUint16(frame, crc16CCITTFalse(frame)), nil
}

// crc16CCITTFalse computes CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF,
// no reflection, no final XOR).
func crc16CCITTFalse(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
