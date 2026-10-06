package venues

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// maxFixtureStem caps the readable part of a fixture file name.
const maxFixtureStem = 96

// FixtureFileName maps a RawResponse endpoint to the file name it is recorded
// under in testdata/fixtures/<venue>/. The name is a readable, filesystem-safe
// form of the endpoint plus a short hash of the exact endpoint, so distinct
// endpoints never collide and the fixture server can find a file from a request.
// Example: "/events?series_ticker=KXBTCD" -> "events_series_ticker-KXBTCD.3f2a9c1e.json".
func FixtureFileName(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	var b strings.Builder
	for _, r := range strings.Trim(endpoint, "/") {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		case r == '=':
			b.WriteByte('-')
		default:
			b.WriteByte('_')
		}
		if b.Len() >= maxFixtureStem {
			break
		}
	}
	stem := strings.Trim(b.String(), "._-")
	if stem == "" {
		stem = "root"
	}
	return stem + "." + hex.EncodeToString(sum[:4]) + ".json"
}
