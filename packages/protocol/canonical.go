package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
)

// actionHashLabel separates action hashes from every other hash in Scopra
// (ADR-008). Its version changes with the canonical form or the algorithm.
const actionHashLabel = "scopra/execution.request/v1\n"

// CanonicalJSON returns the canonical form of r (ADR-008): its ADR-007 wire
// object serialized with the JSON Canonicalization Scheme (RFC 8785). The form
// is computed from the typed value, never from transmitted bytes, and it
// decodes back to r; nil Arguments is written as []. It fails for an invalid
// request.
func (r ExecutionRequest) CanonicalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	arguments := []byte{'['}
	for i, argument := range r.Command.Arguments {
		if i > 0 {
			arguments = append(arguments, ',')
		}
		arguments = appendCanonicalString(arguments, argument)
	}
	arguments = append(arguments, ']')

	return canonicalObject(
		canonicalMember{"executionId", canonicalString(r.ExecutionID)},
		canonicalMember{"principals", canonicalObject(
			canonicalMember{"userId", canonicalString(r.Principals.UserID)},
			canonicalMember{"agentId", canonicalString(r.Principals.AgentID)},
			canonicalMember{"clientId", canonicalString(r.Principals.ClientID)},
		)},
		canonicalMember{"resources", canonicalObject(
			canonicalMember{"machineId", canonicalString(r.Resources.MachineID)},
			canonicalMember{"workspaceId", canonicalString(r.Resources.WorkspaceID)},
		)},
		canonicalMember{"grantId", canonicalString(r.GrantID)},
		canonicalMember{"sessionId", canonicalString(r.SessionID)},
		canonicalMember{"command", canonicalObject(
			canonicalMember{"executable", canonicalString(r.Command.Executable)},
			canonicalMember{"arguments", arguments},
			canonicalMember{"shell", strconv.AppendBool(nil, r.Command.Shell)},
			canonicalMember{"workingDirectory", canonicalString(r.Command.WorkingDirectory)},
		)},
		canonicalMember{"workspaceAccess", canonicalString(string(r.WorkspaceAccess))},
		canonicalMember{"network", canonicalString(string(r.Network))},
		canonicalMember{"limits", canonicalObject(
			canonicalMember{"timeoutSeconds", strconv.AppendUint(nil, r.Limits.TimeoutSeconds, 10)},
			canonicalMember{"cpuMillicores", strconv.AppendUint(nil, r.Limits.CPUMillicores, 10)},
			canonicalMember{"memoryBytes", strconv.AppendUint(nil, r.Limits.MemoryBytes, 10)},
			canonicalMember{"pids", strconv.AppendUint(nil, r.Limits.PIDs, 10)},
			canonicalMember{"outputBytes", strconv.AppendUint(nil, r.Limits.OutputBytes, 10)},
		)},
		canonicalMember{"reason", canonicalString(r.Reason)},
		canonicalMember{"createdAt", canonicalString(r.CreatedAt.Format(requestTimeLayout))},
		canonicalMember{"expiresAt", canonicalString(r.ExpiresAt.Format(requestTimeLayout))},
		canonicalMember{"nonce", canonicalString(r.Nonce)},
	), nil
}

// ActionHash returns the action hash of r (ADR-008): the lowercase hexadecimal
// SHA-256 of the label "scopra/execution.request/v1\n" followed by the
// canonical form. It identifies the action and authenticates nothing; a
// receiver must compute it from the request it decoded and never trust a
// transmitted one.
func (r ExecutionRequest) ActionHash() (string, error) {
	canonical, err := r.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	digest.Write([]byte(actionHashLabel))
	digest.Write(canonical)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// canonicalMember is an object member whose value is already canonical JSON.
type canonicalMember struct {
	key   string
	value []byte
}

// canonicalObject writes members sorted by key, as RFC 8785 requires. Keys are
// compared bytewise, which equals RFC 8785's UTF-16 order because every key in
// this schema is ASCII.
func canonicalObject(members ...canonicalMember) []byte {
	slices.SortFunc(members, func(a, b canonicalMember) int { return strings.Compare(a.key, b.key) })
	object := []byte{'{'}
	for i, member := range members {
		if i > 0 {
			object = append(object, ',')
		}
		object = appendCanonicalString(object, member.key)
		object = append(object, ':')
		object = append(object, member.value...)
	}
	return append(object, '}')
}

func canonicalString(s string) []byte {
	return appendCanonicalString(nil, s)
}

// appendCanonicalString appends s as a JSON string with RFC 8785 escaping: \"
// and \\, the short forms \b \t \n \f \r, \u00xx with lowercase digits for the
// other characters below U+0020, and every other character, including U+007F,
// U+2028, U+2029, and all non-ASCII characters, as literal UTF-8. s must be
// valid UTF-8, which Validate guarantees; bytes of multibyte characters are at
// least 0x80 and are copied unchanged.
func appendCanonicalString(b []byte, s string) []byte {
	const lowerHex = "0123456789abcdef"
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch char := s[i]; char {
		case '"', '\\':
			b = append(b, '\\', char)
		case '\b':
			b = append(b, '\\', 'b')
		case '\t':
			b = append(b, '\\', 't')
		case '\n':
			b = append(b, '\\', 'n')
		case '\f':
			b = append(b, '\\', 'f')
		case '\r':
			b = append(b, '\\', 'r')
		default:
			if char < 0x20 {
				b = append(b, '\\', 'u', '0', '0', lowerHex[char>>4], lowerHex[char&0x0f])
			} else {
				b = append(b, char)
			}
		}
	}
	return append(b, '"')
}
