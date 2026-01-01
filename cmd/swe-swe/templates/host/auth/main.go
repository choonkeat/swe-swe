package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	cookieName      = "swe_swe_session"
	cookieDelimiter = "|"
)

// signCookie creates an HMAC-signed cookie value.
// Format: "timestamp|hmac-signature"
func signCookie(secret string) string {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := computeHMAC(timestamp, secret)
	return timestamp + cookieDelimiter + signature
}

// verifyCookie validates an HMAC-signed cookie value.
func verifyCookie(cookie, secret string) bool {
	if cookie == "" {
		return false
	}

	parts := strings.SplitN(cookie, cookieDelimiter, 2)
	if len(parts) != 2 {
		return false
	}

	timestamp := parts[0]
	signature := parts[1]

	expectedSignature := computeHMAC(timestamp, secret)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// computeHMAC generates an HMAC-SHA256 signature.
func computeHMAC(data, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func main() {
}
