package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

func New() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return formatV4(value)
}

// FromStableKey derives an opaque, canonical identifier for an idempotent
// operation. The namespace prevents the same key being reused across resource
// types. Callers must supply an already non-sensitive operation key.
func FromStableKey(namespace, key string) string {
	digest := sha256.Sum256([]byte(namespace + "\x00" + key))
	var value [16]byte
	copy(value[:], digest[:16])
	return formatV4(value)
}

func formatV4(value [16]byte) string {
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	buffer := make([]byte, 36)
	hex.Encode(buffer[0:8], value[0:4])
	buffer[8] = '-'
	hex.Encode(buffer[9:13], value[4:6])
	buffer[13] = '-'
	hex.Encode(buffer[14:18], value[6:8])
	buffer[18] = '-'
	hex.Encode(buffer[19:23], value[8:10])
	buffer[23] = '-'
	hex.Encode(buffer[24:36], value[10:16])
	return string(buffer)
}

// Valid reports whether value is a canonical RFC 4122 version-4 UUID as
// emitted by New. Keeping resource identifiers canonical bounds storage keys,
// log fields, and metric route templates at every trust boundary.
func Valid(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '4' {
		return false
	}
	if value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b' {
		return false
	}
	for index, character := range []byte(value) {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !isLowerHex(character) {
			return false
		}
	}
	return true
}

func isLowerHex(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'a' && character <= 'f'
}
