package localipc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SynergyFlowLabs/scopra/internal/journal"
	"github.com/SynergyFlowLabs/scopra/packages/protocol"
)

// `execution.logs` reads Mio's run journal (ADR-013). It is a read-only
// extension of IPC v1, like the workspace commands.
const (
	logsCommand = "execution.logs"
	// DefaultLogLimit is how many entries a client asks for by default.
	DefaultLogLimit = journal.DefaultLimit
	// MaxLogLimit is the most entries one request may ask for.
	MaxLogLimit = journal.MaxLimit
	// journalUnavailableMessage is the fixed text of a run refused because
	// Mio cannot record it.
	journalUnavailableMessage = "Mio could not record its run journal"
)

// RunJournal is Mio's run journal as the daemon needs it (ADR-013).
// *journal.Journal implements it.
type RunJournal interface {
	// Append records one answered result. A failure makes every later
	// Append fail, and Broken then reports true.
	Append(workspaceID string, result protocol.ExecutionResult) error
	// Entries returns up to limit entries, newest first.
	Entries(workspaceID string, limit int) ([]journal.Entry, error)
	// Broken reports whether the journal can no longer record.
	Broken() bool
}

// LogFilter selects the entries a Logs call returns.
type LogFilter struct {
	// WorkspaceID limits the result to one workspace when non-empty.
	WorkspaceID string
	// Limit is how many entries to return, from 1 to MaxLogLimit.
	Limit int
}

// Logs returns up to filter.Limit entries from Mio's run journal, newest
// first. It is read-only, and every value is checked before dialing.
func Logs(ctx context.Context, stateDir string, filter LogFilter) ([]journal.Entry, error) {
	if err := validateLogFilter(filter); err != nil {
		return nil, err
	}
	encoded, requestID, err := roundTrip(ctx, stateDir, request{
		Command:         logsCommand,
		LogsWorkspaceID: filter.WorkspaceID,
		LogsLimit:       filter.Limit,
	}, "logs")
	if err != nil {
		return nil, err
	}
	return decodeLogsReply(encoded, requestID)
}

func validateLogFilter(filter LogFilter) error {
	switch {
	case filter.Limit < 1 || filter.Limit > MaxLogLimit:
		return fmt.Errorf("%w: limit must be 1 to %d", ErrInvalidMessage, MaxLogLimit)
	case filter.WorkspaceID != "" && !validWorkspaceID(filter.WorkspaceID):
		return fmt.Errorf("%w: invalid workspace ID", ErrInvalidMessage)
	}
	return nil
}

// replyToLogsCommand answers execution.logs from the journal that execution
// runs record to. Without that journal the command is unknown.
func (d *Daemon) replyToLogsCommand(message request) ([]byte, error) {
	if d.journal == nil {
		return nil, fmt.Errorf("%w: execution.logs is not enabled", ErrInvalidMessage)
	}
	entries, err := d.journal.Entries(message.LogsWorkspaceID, message.LogsLimit)
	if err != nil {
		return encodeErrorReply(message.RequestID, err)
	}
	return encodeLogsReply(message.RequestID, entries)
}

type logsReply struct {
	ProtocolVersion int             `json:"protocolVersion"`
	RequestID       string          `json:"requestId"`
	Entries         []journal.Entry `json:"entries"`
}

func encodeLogsReply(requestID string, entries []journal.Entry) ([]byte, error) {
	if entries == nil {
		entries = []journal.Entry{}
	}
	return encodeReply(logsReply{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		Entries:         entries,
	})
}

// decodeLogsReply decodes the reply and every entry strictly. An error reply
// comes back as a *ReplyError.
func decodeLogsReply(encoded []byte, requestID string) ([]journal.Entry, error) {
	fields, err := decodeReplyFields(encoded, requestID, "entries")
	if err != nil {
		return nil, err
	}
	var rawEntries []json.RawMessage
	if err := json.Unmarshal(fields["entries"], &rawEntries); err != nil || rawEntries == nil {
		return nil, fmt.Errorf("%w: missing entries array", ErrInvalidMessage)
	}
	entries := make([]journal.Entry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		entry, err := journal.DecodeEntry(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidMessage, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
