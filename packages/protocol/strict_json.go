package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"
)

// payloadKind applies the strict-document rules of ADR-007, which ADR-009
// reuses, to one payload type. Every error it returns wraps invalid, so a
// caller can tell which payload failed.
type payloadKind struct {
	invalid error
}

var (
	requestPayload = payloadKind{invalid: ErrInvalidExecutionRequest}
	resultPayload  = payloadKind{invalid: ErrInvalidExecutionResult}
)

// payloadField is one field to decode: its raw JSON, its path for errors, and
// a pointer to its typed target.
type payloadField struct {
	raw    json.RawMessage
	path   string
	target any
}

// checkDocument enforces the document rules: the size bound, exactly one JSON
// object, valid UTF-8, \u escapes that decode to a Unicode scalar value other
// than NUL, no duplicate keys, and no null. The JSON decoder would otherwise
// replace bad escapes with U+FFFD, keep the last duplicate, and treat null as
// a zero value.
func (k payloadKind) checkDocument(payload []byte) error {
	if len(payload) > MaxPayloadBytes {
		return fmt.Errorf("%w: %w", k.invalid, ErrPayloadTooLarge)
	}
	if !utf8.Valid(payload) {
		return k.fieldError("payload", "is not valid UTF-8")
	}
	if !json.Valid(payload) || firstNonSpace(payload) != '{' {
		return k.fieldError("payload", "must be a single JSON object")
	}
	if !validStringEscapes(payload) {
		return k.fieldError("payload", "contains a \\u escape that is NUL or not a Unicode scalar value")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	return k.scanValue(decoder, "", 0)
}

// scanValue walks one JSON value, rejecting null and duplicate keys.
// fieldPath names the value for errors; it is quoted because unknown keys are
// untrusted text.
func (k payloadKind) scanValue(decoder *json.Decoder, fieldPath string, depth int) error {
	if depth > maxJSONDepth {
		return k.fieldError("payload", "nesting exceeds the limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return k.fieldError("payload", "is malformed JSON")
	}
	if token == nil {
		return fmt.Errorf("%w: %q: must not be null", k.invalid, fieldPath)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return k.fieldError("payload", "is malformed JSON")
			}
			child := joinFieldPath(fieldPath, key)
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("%w: %w: %q", k.invalid, ErrDuplicateJSONKey, child)
			}
			seen[key] = struct{}{}
			if err := k.scanValue(decoder, child, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := k.scanValue(decoder, fieldPath, depth+1); err != nil {
				return err
			}
		}
	default:
		return k.fieldError("payload", "is malformed JSON")
	}
	if _, err := decoder.Token(); err != nil {
		return k.fieldError("payload", "is malformed JSON")
	}
	return nil
}

// objectFields decodes the object at fieldPath and requires exactly keys.
func (k payloadKind) objectFields(raw []byte, fieldPath string, keys ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, k.fieldError(fieldPath, "must be an object")
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return nil, k.fieldError(joinFieldPath(fieldPath, key), "is required")
		}
	}
	if len(object) != len(keys) {
		var unknown []string
		for key := range object {
			if !slices.Contains(keys, key) {
				unknown = append(unknown, key)
			}
		}
		slices.Sort(unknown)
		return nil, fmt.Errorf("%w: unknown field %q", k.invalid, joinFieldPath(fieldPath, unknown[0]))
	}
	return object, nil
}

// decodeFields decodes each field into its typed target.
func (k payloadKind) decodeFields(fields []payloadField) error {
	for _, field := range fields {
		if err := json.Unmarshal(field.raw, field.target); err != nil {
			return k.fieldError(field.path, "has the wrong type")
		}
	}
	return nil
}

func (k payloadKind) fieldError(field, problem string) error {
	if field == "" {
		field = "payload"
	}
	return fmt.Errorf("%w: %s: %s", k.invalid, field, problem)
}

// validStringEscapes reports whether every \u escape in the valid JSON
// document payload decodes to a Unicode scalar value other than NUL: a high
// surrogate must be followed by a low one, and a low one must not stand alone.
func validStringEscapes(payload []byte) bool {
	inString := false
	for i := 0; i < len(payload); i++ {
		switch char := payload[i]; {
		case !inString:
			inString = char == '"'
		case char == '"':
			inString = false
		case char == '\\':
			if i+1 >= len(payload) {
				return false
			}
			i++
			if payload[i] != 'u' {
				continue
			}
			code, ok := hexEscape(payload, i+1)
			if !ok {
				return false
			}
			i += 4
			switch {
			case code == 0, code >= 0xDC00 && code <= 0xDFFF:
				return false
			case code >= 0xD800 && code <= 0xDBFF:
				if i+2 >= len(payload) || payload[i+1] != '\\' || payload[i+2] != 'u' {
					return false
				}
				low, ok := hexEscape(payload, i+3)
				if !ok || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

// hexEscape decodes the four hexadecimal digits at payload[start:].
func hexEscape(payload []byte, start int) (rune, bool) {
	if start < 0 || start+4 > len(payload) {
		return 0, false
	}
	var code rune
	for _, char := range payload[start : start+4] {
		var digit byte
		switch {
		case char >= '0' && char <= '9':
			digit = char - '0'
		case char >= 'a' && char <= 'f':
			digit = char - 'a' + 10
		case char >= 'A' && char <= 'F':
			digit = char - 'A' + 10
		default:
			return 0, false
		}
		code = code<<4 | rune(digit)
	}
	return code, true
}

func joinFieldPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
