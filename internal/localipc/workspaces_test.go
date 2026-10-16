package localipc

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

const (
	testRequestID   = "0123456789abcdef0123456789abcdef"
	testWorkspaceID = "fedcba9876543210fedcba9876543210"
	requestPrefix   = `{"protocolVersion":1,"requestId":"` + testRequestID + `",`
	replyPrefix     = `{"protocolVersion":1,"requestId":"` + testRequestID + `",`
)

func TestWorkspaceRequestEncodings(t *testing.T) {
	base := request{ProtocolVersion: protocolVersion, RequestID: testRequestID}
	tests := []struct {
		name    string
		message request
		want    string
	}{
		{
			name:    "status is unchanged",
			message: withCommand(base, statusCommand),
			want:    validStatusRequest,
		},
		{
			name:    "add",
			message: withPath(withCommand(base, addWorkspaceCommand), "/srv/project"),
			want:    requestPrefix + `"command":"workspace.add","path":"/srv/project"}`,
		},
		{
			name:    "first list page",
			message: withCommand(base, listWorkspacesCommand),
			want:    requestPrefix + `"command":"workspace.list","cursor":""}`,
		},
		{
			name:    "next list page",
			message: withCursor(withCommand(base, listWorkspacesCommand), testWorkspaceID),
			want:    requestPrefix + `"command":"workspace.list","cursor":"` + testWorkspaceID + `"}`,
		},
		{
			name:    "remove",
			message: withID(withCommand(base, removeWorkspaceCommand), testWorkspaceID),
			want:    requestPrefix + `"command":"workspace.remove","id":"` + testWorkspaceID + `"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := encodeRequest(test.message)
			if err != nil {
				t.Fatalf("encodeRequest() error = %v", err)
			}
			if string(encoded) != test.want {
				t.Fatalf("encodeRequest() = %s, want %s", encoded, test.want)
			}
			decoded, err := decodeRequest(encoded)
			if err != nil {
				t.Fatalf("decodeRequest(encodeRequest()) error = %v", err)
			}
			if decoded != test.message {
				t.Fatalf("decodeRequest(encodeRequest()) = %+v, want %+v", decoded, test.message)
			}
		})
	}
}

func TestSecurity_DecodeRequestRejectsMalformedWorkspaceRequests(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "status with a workspace field", data: requestPrefix + `"command":"status","path":"/srv/a"}`},
		{name: "unknown workspace command", data: requestPrefix + `"command":"workspace.update","path":"/srv/a"}`},
		{name: "command is not a string", data: requestPrefix + `"command":1}`},
		{name: "missing command", data: `{"protocolVersion":1,"requestId":"` + testRequestID + `"}`},
		{name: "add without a path", data: requestPrefix + `"command":"workspace.add"}`},
		{name: "add with an extra field", data: requestPrefix + `"command":"workspace.add","path":"/srv/a","id":"` + testWorkspaceID + `"}`},
		{name: "add with a wrongly cased key", data: requestPrefix + `"command":"workspace.add","Path":"/srv/a"}`},
		{name: "add with duplicate paths", data: requestPrefix + `"command":"workspace.add","path":"/srv/a","path":"/srv/b"}`},
		{name: "add with an empty path", data: requestPrefix + `"command":"workspace.add","path":""}`},
		{name: "add with a null path", data: requestPrefix + `"command":"workspace.add","path":null}`},
		{name: "add with a non-string path", data: requestPrefix + `"command":"workspace.add","path":["/srv/a"]}`},
		{name: "add with a path over 4095 bytes", data: requestPrefix + `"command":"workspace.add","path":"/` + strings.Repeat("a", registry.MaxPathBytes) + `"}`},
		// The JSON decoder would silently replace the byte, naming another directory.
		{name: "add with invalid UTF-8", data: requestPrefix + "\"command\":\"workspace.add\",\"path\":\"/srv/\xff\"}"},
		{name: "list without a cursor", data: requestPrefix + `"command":"workspace.list"}`},
		{name: "list with a null cursor", data: requestPrefix + `"command":"workspace.list","cursor":null}`},
		{name: "list with a short cursor", data: requestPrefix + `"command":"workspace.list","cursor":"abc"}`},
		{name: "list with an uppercase cursor", data: requestPrefix + `"command":"workspace.list","cursor":"` + strings.ToUpper(testWorkspaceID) + `"}`},
		{name: "remove without an ID", data: requestPrefix + `"command":"workspace.remove"}`},
		{name: "remove with a malformed ID", data: requestPrefix + `"command":"workspace.remove","id":"not-an-id"}`},
		{name: "remove with an uppercase ID", data: requestPrefix + `"command":"workspace.remove","id":"` + strings.ToUpper(testWorkspaceID) + `"}`},
		{name: "remove with a cursor instead of an ID", data: requestPrefix + `"command":"workspace.remove","cursor":"` + testWorkspaceID + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if message, err := decodeRequest([]byte(test.data)); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("decodeRequest() = %+v, %v; want ErrInvalidMessage", message, err)
			}
		})
	}
}

func TestEncodeRequestRejectsInvalidWorkspaceFields(t *testing.T) {
	base := request{ProtocolVersion: protocolVersion, RequestID: testRequestID}
	tests := []struct {
		name    string
		message request
	}{
		{name: "add with an empty path", message: withCommand(base, addWorkspaceCommand)},
		{name: "add with invalid UTF-8", message: withPath(withCommand(base, addWorkspaceCommand), "/srv/\xff")},
		{name: "add with a path over 4095 bytes", message: withPath(withCommand(base, addWorkspaceCommand), "/"+strings.Repeat("a", registry.MaxPathBytes))},
		{name: "list with a malformed cursor", message: withCursor(withCommand(base, listWorkspacesCommand), "abc")},
		{name: "remove with a malformed ID", message: withID(withCommand(base, removeWorkspaceCommand), "abc")},
		{name: "status with a path", message: withPath(withCommand(base, statusCommand), "/srv/a")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if encoded, err := encodeRequest(test.message); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("encodeRequest() = %s, %v; want ErrInvalidMessage", encoded, err)
			}
		})
	}
}

func TestWorkspacePagesFitFramesAndListEveryRecordOnce(t *testing.T) {
	// The longest storable paths, made of a character that JSON escapes to six
	// bytes, are the worst case for a page.
	records := make([]Workspace, 0, registry.MaxWorkspaces)
	for index := range registry.MaxWorkspaces {
		records = append(records, Workspace{
			// Descending IDs, so that paging must sort by ID.
			ID:   fmt.Sprintf("%032x", registry.MaxWorkspaces-index),
			Path: fmt.Sprintf("/%03d/%s", index, strings.Repeat("\x01", registry.MaxPathBytes-5)),
		})
	}

	var listed []string
	cursor := ""
	for pages := 1; ; pages++ {
		if pages > len(records) {
			t.Fatal("paging did not finish within one page per record")
		}
		page, next := workspacePage(records, cursor, listPageBudget)
		if len(page) == 0 {
			t.Fatalf("page %d is empty while records remain", pages)
		}
		encoded, err := encodeListReply(testRequestID, page, next)
		if err != nil {
			t.Fatalf("encodeListReply(page %d) error = %v", pages, err)
		}
		if len(encoded) > MaxFrameBytes {
			t.Fatalf("page %d encodes to %d bytes, more than one frame", pages, len(encoded))
		}
		decoded, decodedNext, err := decodeListReply(encoded, testRequestID, cursor)
		if err != nil {
			t.Fatalf("decodeListReply(page %d) error = %v", pages, err)
		}
		if !slices.Equal(decoded, page) || decodedNext != next {
			t.Fatalf("page %d did not survive a round trip", pages)
		}
		for _, record := range page {
			listed = append(listed, record.ID)
		}
		if next == "" {
			if pages < 2 {
				t.Fatalf("the worst case fit in %d page, want several", pages)
			}
			break
		}
		cursor = next
	}

	want := make([]string, 0, len(records))
	for _, record := range records {
		want = append(want, record.ID)
	}
	slices.Sort(want)
	if !slices.Equal(listed, want) {
		t.Fatalf("paging listed %d IDs, want each of the %d IDs once in ascending order", len(listed), len(want))
	}
}

func TestWorkspacePagesFitFramesWhenRecordsFillThePage(t *testing.T) {
	// Sweep path lengths so that some page fills its budget almost exactly:
	// the rest of the reply must still fit in the frame.
	for length := 4000; length <= registry.MaxPathBytes; length++ {
		records := make([]Workspace, 0, 17)
		for index := range 17 {
			records = append(records, Workspace{
				ID:   fmt.Sprintf("%032x", index+1),
				Path: fmt.Sprintf("/%02d/%s", index, strings.Repeat("p", length-4)),
			})
		}
		for cursor := ""; ; {
			page, next := workspacePage(records, cursor, listPageBudget)
			encoded, err := encodeListReply(testRequestID, page, next)
			if err != nil || len(encoded) > MaxFrameBytes {
				t.Fatalf("encodeListReply(%d records of %d-byte paths) = %d bytes, %v; want at most one frame",
					len(page), length, len(encoded), err)
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
}

func TestWorkspacePageEdges(t *testing.T) {
	a := Workspace{ID: strings.Repeat("a", 32), Path: "/srv/a"}
	b := Workspace{ID: strings.Repeat("b", 32), Path: "/srv/b"}
	tests := []struct {
		name     string
		records  []Workspace
		cursor   string
		budget   int
		wantPage []Workspace
		wantNext string
	}{
		{name: "empty registry", budget: listPageBudget},
		{name: "everything fits", records: []Workspace{b, a}, budget: listPageBudget, wantPage: []Workspace{a, b}},
		{name: "records after the cursor", records: []Workspace{b, a}, cursor: a.ID, budget: listPageBudget, wantPage: []Workspace{b}},
		{name: "cursor of a removed record", records: []Workspace{a, b}, cursor: strings.Repeat("a", 31) + "b", budget: listPageBudget, wantPage: []Workspace{b}},
		{name: "cursor past the end", records: []Workspace{a, b}, cursor: strings.Repeat("c", 32), budget: listPageBudget},
		{name: "at least one record per page", records: []Workspace{a, b}, budget: 1, wantPage: []Workspace{a}, wantNext: a.ID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, next := workspacePage(test.records, test.cursor, test.budget)
			if !slices.Equal(page, test.wantPage) || next != test.wantNext {
				t.Fatalf("workspacePage() = %+v, %q; want %+v, %q", page, next, test.wantPage, test.wantNext)
			}
		})
	}
}

func TestWorkspaceErrorCodes(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: fmt.Errorf("%w: \"relative\"", workspace.ErrInvalidPath), want: CodeInvalidPath},
		{err: fmt.Errorf("%w: lstat /srv/missing: no such file or directory", workspace.ErrUnresolvable), want: CodeUnresolvablePath},
		{err: workspace.ErrNotDirectory, want: CodeNotDirectory},
		{err: fmt.Errorf("%w: \"/home/alice\"", workspace.ErrProtectedPath), want: CodeProtectedPath},
		{err: workspace.ErrNotOwned, want: CodeNotOwned},
		{err: registry.ErrAlreadyRegistered, want: CodeAlreadyRegistered},
		{err: registry.ErrOverlap, want: CodeOverlap},
		{err: registry.ErrRegistryFull, want: CodeRegistryFull},
		{err: registry.ErrNotFound, want: CodeNotFound},
		{err: errors.New("replace registry file: no space left on device"), want: CodeInternal},
	}
	for _, test := range tests {
		if got := workspaceErrorCode(test.err); got != test.want {
			t.Errorf("workspaceErrorCode(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}

func TestErrorRepliesCarryBoundedValidMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		// The leading byte makes a cut at 1 KiB fall inside a two-byte character.
		{name: "multibyte message over 1 KiB", err: errors.New("a" + strings.Repeat("é", maxErrorMessageBytes))},
		{name: "invalid UTF-8", err: errors.New("lstat /srv/\xff: permission denied")},
		// Each invalid byte would grow to a three-byte replacement character if
		// the message were cut before being made valid.
		{name: "invalid UTF-8 over 1 KiB", err: errors.New(strings.Repeat("x\xff", maxErrorMessageBytes))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := encodeErrorReply(testRequestID, test.err)
			if err != nil {
				t.Fatalf("encodeErrorReply() error = %v", err)
			}
			_, err = decodeWorkspaceReply(encoded, testRequestID)
			var replyErr *WorkspaceError
			if !errors.As(err, &replyErr) {
				t.Fatalf("decodeWorkspaceReply(error reply) error = %v, want a *WorkspaceError", err)
			}
			message := replyErr.Message
			if replyErr.Code != CodeInternal || message == "" || len(message) > maxErrorMessageBytes || !utf8.ValidString(message) {
				t.Fatalf("error reply = %q, %d bytes; want code internal and a non-empty valid message of at most %d bytes",
					replyErr.Code, len(message), maxErrorMessageBytes)
			}
			if prefix, _, _ := strings.Cut(message, "\uFFFD"); !strings.HasPrefix(test.err.Error(), prefix) {
				t.Fatalf("error reply message %q is not taken from %q", message, test.err)
			}
		})
	}
}

func TestDecodeWorkspaceReplyAcceptsFutureErrorCodes(t *testing.T) {
	data := replyPrefix + `"error":{"code":"quota_exceeded","message":"too many workspaces today"}}`
	_, err := decodeWorkspaceReply([]byte(data), testRequestID)
	var replyErr *WorkspaceError
	if !errors.As(err, &replyErr) || replyErr.Code != "quota_exceeded" || replyErr.Message != "too many workspaces today" {
		t.Fatalf("decodeWorkspaceReply() error = %v, want a *WorkspaceError with the unknown code", err)
	}
}

func TestSecurity_DecodeWorkspaceRepliesRejectInvalidReplies(t *testing.T) {
	workspaceJSON := func(id, path string) string { return `{"id":"` + id + `","path":"` + path + `"}` }
	valid := workspaceJSON(testWorkspaceID, "/srv/a")
	tests := []struct {
		name string
		data string
	}{
		{name: "wrong request ID", data: strings.Replace(replyPrefix, testRequestID, strings.Repeat("f", 32), 1) + `"workspace":` + valid + `}`},
		{name: "unsupported version", data: strings.Replace(replyPrefix, `"protocolVersion":1`, `"protocolVersion":2`, 1) + `"workspace":` + valid + `}`},
		{name: "unknown field", data: replyPrefix + `"workspace":` + valid + `,"extra":1}`},
		{name: "workspace and error together", data: replyPrefix + `"workspace":` + valid + `,"error":{"code":"internal","message":"x"}}`},
		{name: "null workspace", data: replyPrefix + `"workspace":null}`},
		{name: "wrongly cased nested key", data: replyPrefix + `"workspace":{"ID":"` + testWorkspaceID + `","path":"/srv/a"}}`},
		{name: "extra nested field", data: replyPrefix + `"workspace":{"id":"` + testWorkspaceID + `","path":"/srv/a","fingerprint":{}}}`},
		{name: "malformed ID", data: replyPrefix + `"workspace":` + workspaceJSON("xyz", "/srv/a") + `}`},
		{name: "relative path", data: replyPrefix + `"workspace":` + workspaceJSON(testWorkspaceID, "srv/a") + `}`},
		{name: "unclean path", data: replyPrefix + `"workspace":` + workspaceJSON(testWorkspaceID, "/srv/../a") + `}`},
		{name: "filesystem root", data: replyPrefix + `"workspace":` + workspaceJSON(testWorkspaceID, "/") + `}`},
		{name: "path over 4095 bytes", data: replyPrefix + `"workspace":` + workspaceJSON(testWorkspaceID, "/"+strings.Repeat("a", registry.MaxPathBytes)) + `}`},
		{name: "invalid UTF-8", data: replyPrefix + `"workspace":` + workspaceJSON(testWorkspaceID, "/srv/\xff") + `}`},
		{name: "malformed error code", data: replyPrefix + `"error":{"code":"Not Found","message":"x"}}`},
		{name: "empty error message", data: replyPrefix + `"error":{"code":"not_found","message":""}}`},
		{name: "error message over 1 KiB", data: replyPrefix + `"error":{"code":"not_found","message":"` + strings.Repeat("x", maxErrorMessageBytes+1) + `"}}`},
		{name: "extra error field", data: replyPrefix + `"error":{"code":"not_found","message":"x","detail":1}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodeWorkspaceReply([]byte(test.data), testRequestID)
			var replyErr *WorkspaceError
			if !errors.Is(err, ErrInvalidMessage) || errors.As(err, &replyErr) {
				t.Fatalf("decodeWorkspaceReply() = %+v, %v; want ErrInvalidMessage", got, err)
			}
		})
	}
}

func TestSecurity_DecodeListRepliesRejectPagesThatDoNotAdvance(t *testing.T) {
	a := `{"id":"` + strings.Repeat("a", 32) + `","path":"/srv/a"}`
	b := `{"id":"` + strings.Repeat("b", 32) + `","path":"/srv/b"}`
	tests := []struct {
		name   string
		cursor string
		data   string
	}{
		{name: "null list", data: replyPrefix + `"workspaces":null,"nextCursor":""}`},
		{name: "missing next cursor", data: replyPrefix + `"workspaces":[]}`},
		{name: "IDs out of order", data: replyPrefix + `"workspaces":[` + b + `,` + a + `],"nextCursor":""}`},
		{name: "repeated ID", data: replyPrefix + `"workspaces":[` + a + `,` + a + `],"nextCursor":""}`},
		{name: "record at the cursor", cursor: strings.Repeat("a", 32), data: replyPrefix + `"workspaces":[` + a + `],"nextCursor":""}`},
		{name: "next cursor is not the last ID", data: replyPrefix + `"workspaces":[` + a + `],"nextCursor":"` + strings.Repeat("b", 32) + `"}`},
		{name: "next cursor after an empty page", data: replyPrefix + `"workspaces":[],"nextCursor":"` + strings.Repeat("a", 32) + `"}`},
		{name: "malformed next cursor", data: replyPrefix + `"workspaces":[` + a + `],"nextCursor":"zz"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, next, err := decodeListReply([]byte(test.data), testRequestID, test.cursor)
			if !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("decodeListReply() = %+v, %q, %v; want ErrInvalidMessage", page, next, err)
			}
		})
	}
}

func withCommand(message request, command string) request {
	message.Command = command
	return message
}

func withPath(message request, path string) request {
	message.Path = path
	return message
}

func withCursor(message request, cursor string) request {
	message.Cursor = cursor
	return message
}

func withID(message request, id string) request {
	message.ID = id
	return message
}
