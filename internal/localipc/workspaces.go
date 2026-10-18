package localipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/SynergyFlowLabs/scopra/internal/workspace"
	"github.com/SynergyFlowLabs/scopra/internal/workspace/registry"
)

// Workspace commands extend IPC v1 (ADR-006).
const (
	addWorkspaceCommand    = "workspace.add"
	listWorkspacesCommand  = "workspace.list"
	removeWorkspaceCommand = "workspace.remove"

	// maxErrorMessageBytes bounds the message of an error reply.
	maxErrorMessageBytes = 1024
	// maxErrorCodeBytes bounds error codes, including codes from newer Mio versions.
	maxErrorCodeBytes = 32
	// listPageBudget bounds the encoded records of one list reply and leaves
	// room in the frame for the rest of the reply.
	listPageBudget = MaxFrameBytes - 512
)

// Error codes in Mio's replies to workspace requests that failed. Newer
// versions of Mio may add codes; clients should treat unknown codes as errors.
const (
	// CodeInvalidPath: the path is not absolute or not valid UTF-8.
	CodeInvalidPath = "invalid_path"
	// CodeUnresolvablePath: the path does not exist or cannot be resolved.
	CodeUnresolvablePath = "unresolvable_path"
	// CodeNotDirectory: the path does not resolve to a directory.
	CodeNotDirectory = "not_directory"
	// CodeProtectedPath: the directory is, contains, or (for Mio's state
	// directory) lies inside a protected location.
	CodeProtectedPath = "protected_path"
	// CodeNotOwned: the directory is not owned by the user running Mio.
	CodeNotOwned = "not_owned"
	// CodeAlreadyRegistered: the directory is already registered.
	CodeAlreadyRegistered = "already_registered"
	// CodeOverlap: the directory is inside, or contains, a registered workspace.
	CodeOverlap = "overlap"
	// CodeRegistryFull: no more workspaces can be registered.
	CodeRegistryFull = "registry_full"
	// CodeNotFound: no workspace is registered under the ID.
	CodeNotFound = "not_found"
	// CodeInternal: Mio failed for another reason, such as an I/O error. A
	// change may still have taken effect; list the workspaces to check.
	CodeInternal = "internal"
)

// Workspace is a registered workspace as reported by Mio.
type Workspace struct {
	// ID is the registration's random identifier in lowercase hexadecimal.
	ID string `json:"id"`
	// Path is the workspace's canonical absolute path.
	Path string `json:"path"`
}

// ReplyError is Mio's reply to a well-formed local IPC request that failed.
// Message is diagnostic text from Mio; escape it before displaying it.
type ReplyError struct {
	Code    string
	Message string
}

func (e *ReplyError) Error() string {
	return "localipc: Mio reported " + e.Code + ": " + e.Message
}

// WorkspaceRegistry is the registry that Mio serves workspace commands from.
// *registry.Registry implements it. Its methods must be safe for concurrent use.
type WorkspaceRegistry interface {
	Add(path string) (registry.Record, error)
	Get(id string) (registry.Record, error)
	Remove(id string) error
	List() []registry.Record
}

// EnableWorkspaces makes the daemon serve workspace commands from workspaces,
// which must be open in the daemon's locked state directory. It must be called
// before Serve; until then, workspace commands are treated as unknown.
func (d *Daemon) EnableWorkspaces(workspaces WorkspaceRegistry) error {
	if d == nil || workspaces == nil {
		return errors.New("localipc: a daemon and a workspace registry are required")
	}
	d.serveMu.Lock()
	defer d.serveMu.Unlock()
	if d.served {
		return ErrDaemonAlreadyServing
	}
	d.workspaces = workspaces
	return nil
}

// AddWorkspace asks Mio to register the directory at path. Mio rejects
// relative paths and replies with the canonical path that it registered.
func AddWorkspace(ctx context.Context, stateDir, path string) (Workspace, error) {
	encoded, requestID, err := roundTrip(ctx, stateDir,
		request{Command: addWorkspaceCommand, Path: path}, "workspace")
	if err != nil {
		return Workspace{}, err
	}
	return decodeWorkspaceReply(encoded, requestID)
}

// RemoveWorkspace asks Mio to remove the registration with the given ID and
// returns it. The workspace directory itself is never touched.
func RemoveWorkspace(ctx context.Context, stateDir, id string) (Workspace, error) {
	encoded, requestID, err := roundTrip(ctx, stateDir,
		request{Command: removeWorkspaceCommand, ID: id}, "workspace")
	if err != nil {
		return Workspace{}, err
	}
	return decodeWorkspaceReply(encoded, requestID)
}

// ListWorkspaces returns every registered workspace, sorted by path. It
// follows Mio's pages, so the result is not a snapshot if the registry changes
// meanwhile.
func ListWorkspaces(ctx context.Context, stateDir string) ([]Workspace, error) {
	all := []Workspace{}
	cursor := ""
	for {
		page, next, err := listWorkspacesPage(ctx, stateDir, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		// Pages always advance, so this bound also ends the loop.
		if len(all) > registry.MaxWorkspaces {
			return nil, fmt.Errorf("%w: Mio listed more than %d workspaces",
				ErrInvalidMessage, registry.MaxWorkspaces)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	slices.SortFunc(all, func(a, b Workspace) int { return strings.Compare(a.Path, b.Path) })
	return all, nil
}

func listWorkspacesPage(ctx context.Context, stateDir, cursor string) ([]Workspace, string, error) {
	encoded, requestID, err := roundTrip(ctx, stateDir,
		request{Command: listWorkspacesCommand, Cursor: cursor}, "workspace list")
	if err != nil {
		return nil, "", err
	}
	return decodeListReply(encoded, requestID, cursor)
}

// replyToWorkspaceCommand runs a decoded workspace request against the
// enabled registry. Failures become error replies.
func (d *Daemon) replyToWorkspaceCommand(message request) ([]byte, error) {
	switch message.Command {
	case addWorkspaceCommand:
		record, err := d.workspaces.Add(message.Path)
		if err != nil {
			return encodeErrorReply(message.RequestID, err)
		}
		return encodeWorkspaceReply(message.RequestID, workspaceOf(record))
	case removeWorkspaceCommand:
		record, err := d.workspaces.Get(message.ID)
		if err == nil {
			err = d.workspaces.Remove(message.ID)
		}
		if err != nil {
			return encodeErrorReply(message.RequestID, err)
		}
		return encodeWorkspaceReply(message.RequestID, workspaceOf(record))
	case listWorkspacesCommand:
		records := d.workspaces.List()
		workspaces := make([]Workspace, 0, len(records))
		for _, record := range records {
			workspaces = append(workspaces, workspaceOf(record))
		}
		page, next := workspacePage(workspaces, message.Cursor, listPageBudget)
		return encodeListReply(message.RequestID, page, next)
	default:
		return nil, fmt.Errorf("%w: unsupported command", ErrInvalidMessage)
	}
}

func workspaceOf(record registry.Record) Workspace {
	return Workspace{ID: record.ID, Path: record.Path}
}

// workspacePage returns, in ID order, the records after cursor whose encoded
// size fits in budget, and the cursor for the next page, which is empty when
// no records remain. A page holds at least one record while any remain.
func workspacePage(records []Workspace, cursor string, budget int) ([]Workspace, string) {
	sorted := slices.Clone(records)
	slices.SortFunc(sorted, func(a, b Workspace) int { return strings.Compare(a.ID, b.ID) })
	page := []Workspace{}
	used := 0
	for _, record := range sorted {
		if record.ID <= cursor {
			continue
		}
		encoded, _ := json.Marshal(record) // marshaling strings cannot fail
		size := len(encoded) + 1           // and a separating comma
		if len(page) > 0 && used+size > budget {
			return page, page[len(page)-1].ID
		}
		page = append(page, record)
		used += size
	}
	return page, ""
}

type workspaceReply struct {
	ProtocolVersion int       `json:"protocolVersion"`
	RequestID       string    `json:"requestId"`
	Workspace       Workspace `json:"workspace"`
}

type listReply struct {
	ProtocolVersion int         `json:"protocolVersion"`
	RequestID       string      `json:"requestId"`
	Workspaces      []Workspace `json:"workspaces"`
	NextCursor      string      `json:"nextCursor"`
}

type errorReply struct {
	ProtocolVersion int        `json:"protocolVersion"`
	RequestID       string     `json:"requestId"`
	Error           replyError `json:"error"`
}

type replyError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func encodeWorkspaceReply(requestID string, record Workspace) ([]byte, error) {
	return encodeReply(workspaceReply{ProtocolVersion: protocolVersion, RequestID: requestID, Workspace: record})
}

func encodeListReply(requestID string, page []Workspace, next string) ([]byte, error) {
	if page == nil {
		page = []Workspace{}
	}
	return encodeReply(listReply{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		Workspaces:      page,
		NextCursor:      next,
	})
}

// encodeErrorReply reports err with its code and a valid UTF-8 message of at
// most maxErrorMessageBytes.
func encodeErrorReply(requestID string, err error) ([]byte, error) {
	code := workspaceErrorCode(err)
	message := truncateUTF8(strings.ToValidUTF8(err.Error(), "\uFFFD"), maxErrorMessageBytes)
	if message == "" {
		message = code
	}
	return encodeReply(errorReply{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		Error:           replyError{Code: code, Message: message},
	})
}

func encodeReply(reply any) ([]byte, error) {
	encoded, err := json.Marshal(reply)
	if err != nil {
		return nil, fmt.Errorf("encode reply: %w", err)
	}
	if len(encoded) > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	return encoded, nil
}

// truncateUTF8 shortens valid UTF-8 to at most limit bytes without splitting
// a character.
func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func workspaceErrorCode(err error) string {
	switch {
	case errors.Is(err, workspace.ErrInvalidPath):
		return CodeInvalidPath
	case errors.Is(err, workspace.ErrUnresolvable):
		return CodeUnresolvablePath
	case errors.Is(err, workspace.ErrNotDirectory):
		return CodeNotDirectory
	case errors.Is(err, workspace.ErrProtectedPath):
		return CodeProtectedPath
	case errors.Is(err, workspace.ErrNotOwned):
		return CodeNotOwned
	case errors.Is(err, registry.ErrAlreadyRegistered):
		return CodeAlreadyRegistered
	case errors.Is(err, registry.ErrOverlap):
		return CodeOverlap
	case errors.Is(err, registry.ErrRegistryFull):
		return CodeRegistryFull
	case errors.Is(err, registry.ErrNotFound):
		return CodeNotFound
	default:
		return CodeInternal
	}
}

func decodeWorkspaceReply(encoded []byte, requestID string) (Workspace, error) {
	fields, err := decodeReplyFields(encoded, requestID, "workspace")
	if err != nil {
		return Workspace{}, err
	}
	return decodeWorkspace(fields["workspace"])
}

// decodeListReply decodes one page and checks that it advances: IDs ascend
// after cursor, and a next cursor names the page's last record.
func decodeListReply(encoded []byte, requestID, cursor string) ([]Workspace, string, error) {
	fields, err := decodeReplyFields(encoded, requestID, "workspaces", "nextCursor")
	if err != nil {
		return nil, "", err
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(fields["workspaces"], &entries); err != nil || entries == nil {
		return nil, "", fmt.Errorf("%w: missing workspace list", ErrInvalidMessage)
	}
	page := make([]Workspace, 0, len(entries))
	previous := cursor
	for _, entry := range entries {
		record, err := decodeWorkspace(entry)
		if err != nil {
			return nil, "", err
		}
		if record.ID <= previous {
			return nil, "", fmt.Errorf("%w: list page does not ascend after its cursor", ErrInvalidMessage)
		}
		page = append(page, record)
		previous = record.ID
	}
	next, err := jsonString(fields["nextCursor"])
	if err != nil {
		return nil, "", err
	}
	if next != "" && (len(page) == 0 || next != page[len(page)-1].ID) {
		return nil, "", fmt.Errorf("%w: list page has an invalid next cursor", ErrInvalidMessage)
	}
	return page, next, nil
}

// decodeReplyFields checks a reply's envelope and returns its fields, which
// must be exactly keys besides protocolVersion and requestId. An error reply
// is returned as a *ReplyError.
func decodeReplyFields(encoded []byte, requestID string, keys ...string) (map[string]json.RawMessage, error) {
	if err := checkJSONDocument(encoded); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("%w: reply must be a JSON object", ErrInvalidMessage)
	}
	expected := append([]string{"protocolVersion", "requestId"}, keys...)
	_, isError := fields["error"]
	if isError {
		expected = []string{"protocolVersion", "requestId", "error"}
	}
	if err := validateObjectKeys(encoded, expected); err != nil {
		return nil, err
	}
	var version int
	if err := json.Unmarshal(fields["protocolVersion"], &version); err != nil || version != protocolVersion {
		return nil, fmt.Errorf("%w: unsupported reply version", ErrInvalidMessage)
	}
	if id, err := jsonString(fields["requestId"]); err != nil || id != requestID {
		return nil, fmt.Errorf("%w: reply does not match the request", ErrInvalidMessage)
	}
	if isError {
		return nil, decodeErrorReply(fields["error"])
	}
	return fields, nil
}

func decodeErrorReply(raw json.RawMessage) error {
	fields, err := decodeObjectFields(raw, "code", "message")
	if err != nil {
		return err
	}
	code, err := jsonString(fields["code"])
	if err != nil {
		return err
	}
	message, err := jsonString(fields["message"])
	if err != nil {
		return err
	}
	if !validErrorCode(code) || message == "" || len(message) > maxErrorMessageBytes {
		return fmt.Errorf("%w: malformed error reply", ErrInvalidMessage)
	}
	return &ReplyError{Code: code, Message: message}
}

func decodeWorkspace(raw json.RawMessage) (Workspace, error) {
	fields, err := decodeObjectFields(raw, "id", "path")
	if err != nil {
		return Workspace{}, err
	}
	id, err := jsonString(fields["id"])
	if err != nil {
		return Workspace{}, err
	}
	path, err := jsonString(fields["path"])
	if err != nil {
		return Workspace{}, err
	}
	if !validWorkspaceID(id) || !validWorkspacePath(path) {
		return Workspace{}, fmt.Errorf("%w: malformed workspace record", ErrInvalidMessage)
	}
	return Workspace{ID: id, Path: path}, nil
}

// decodeObjectFields returns the fields of a nested JSON object, which must
// have exactly the given keys.
func decodeObjectFields(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, error) {
	if err := validateObjectKeys(raw, keys); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%w: expected a JSON object", ErrInvalidMessage)
	}
	return fields, nil
}

// ValidWorkspaceID reports whether id has the format of a workspace ID: 32
// lowercase hexadecimal characters (128 random bits).
func ValidWorkspaceID(id string) bool { return validWorkspaceID(id) }

func validWorkspaceID(id string) bool { return validRequestID(id) }

// validWorkspacePath reports whether path is a canonical workspace path.
func validWorkspacePath(path string) bool {
	return len(path) <= registry.MaxPathBytes && utf8.ValidString(path) &&
		filepath.IsAbs(path) && filepath.Clean(path) == path && path != string(filepath.Separator)
}

func validErrorCode(code string) bool {
	if code == "" || len(code) > maxErrorCodeBytes || code[0] < 'a' || code[0] > 'z' {
		return false
	}
	for _, character := range code {
		if (character < 'a' || character > 'z') && character != '_' {
			return false
		}
	}
	return true
}
