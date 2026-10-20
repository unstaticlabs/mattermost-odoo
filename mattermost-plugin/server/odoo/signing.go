// Package odoo is the plugin side of the API between the plugin and the
// Odoo module usl_mattermost. See docs/api.md of this repository.
package odoo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	signatureScheme = "v1="
	maxSkew         = 5 * time.Minute

	// HeaderTimestamp and HeaderSignature carry the request signature.
	HeaderTimestamp = "X-USL-Timestamp"
	HeaderSignature = "X-USL-Signature"
)

// Sign returns the X-USL-Signature value for one request.
func Sign(key string, timestamp int64, method, path string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("\n"))
	mac.Write([]byte(strings.ToUpper(method)))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return signatureScheme + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks one request. It returns "" when the request is valid, else
// the error code of docs/api.md: "stale_timestamp" or "bad_signature".
func Verify(keys []string, timestampHeader, signatureHeader, method, path string, body []byte, now time.Time) string {
	timestamp, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return "stale_timestamp"
	}
	skew := now.Sub(time.Unix(timestamp, 0))
	if skew > maxSkew || skew < -maxSkew {
		return "stale_timestamp"
	}
	if !strings.HasPrefix(signatureHeader, signatureScheme) {
		return "bad_signature"
	}
	for _, key := range keys {
		if key == "" {
			continue
		}
		expected := Sign(key, timestamp, method, path, body)
		if hmac.Equal([]byte(expected), []byte(signatureHeader)) {
			return ""
		}
	}
	return "bad_signature"
}
