package executionid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	Prefix            = "xe1_"
	MaxRequestIDBytes = 256
	domain            = "xinsight-execution-v1"
)

var (
	uuidPattern        = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
	executionIDPattern = regexp.MustCompile(`^xe1_[a-f0-9]{64}$`)
)

func New(sourceSystemID string, requestID string) (string, error) {
	canonicalSource, err := CanonicalSourceSystemID(sourceSystemID)
	if err != nil {
		return "", err
	}
	if err := ValidateRequestID(requestID); err != nil {
		return "", err
	}

	digest := sha256.Sum256([]byte(domain + "\x00" + canonicalSource + "\x00" + requestID))
	return Prefix + hex.EncodeToString(digest[:]), nil
}

func ValidateRequestID(value string) error {
	return validateText("request_id", value, 1, MaxRequestIDBytes)
}

func ValidateRequestIDPrefix(value string) error {
	return validateText("request_id_prefix", value, 3, 128)
}

func Valid(value string) bool {
	return executionIDPattern.MatchString(value)
}

func CanonicalSourceSystemID(value string) (string, error) {
	if strings.TrimSpace(value) != value || !uuidPattern.MatchString(value) {
		return "", fmt.Errorf("source_system_id must be a canonical UUID")
	}
	return strings.ToLower(value), nil
}

func validateText(name string, value string, minimum int, maximum int) error {
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not contain surrounding whitespace", name)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", name)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s must not contain Unicode control characters", name)
		}
	}
	length := len([]byte(value))
	if length < minimum || length > maximum {
		return fmt.Errorf("%s must contain %d to %d UTF-8 bytes", name, minimum, maximum)
	}
	return nil
}
