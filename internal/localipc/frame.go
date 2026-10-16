package localipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameBytes is the largest accepted length-prefixed IPC payload.
const MaxFrameBytes = 64 << 10

var (
	// ErrInvalidFrame reports an empty, malformed, or truncated IPC frame.
	ErrInvalidFrame = errors.New("localipc: invalid frame")
	// ErrFrameTooLarge reports an IPC payload that exceeds MaxFrameBytes.
	ErrFrameTooLarge = errors.New("localipc: frame exceeds size limit")
)

func readFrame(reader io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, fmt.Errorf("%w: incomplete frame header", ErrInvalidFrame)
	}

	length := binary.BigEndian.Uint32(header[:])
	if length == 0 {
		return nil, fmt.Errorf("%w: empty frame", ErrInvalidFrame)
	}
	if length > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}

	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, fmt.Errorf("%w: incomplete frame payload", ErrInvalidFrame)
	}
	return payload, nil
}

func writeFrame(writer io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: empty frame", ErrInvalidFrame)
	}
	if len(payload) > MaxFrameBytes {
		return ErrFrameTooLarge
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeFull(writer, header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if err := writeFull(writer, payload); err != nil {
		return fmt.Errorf("write frame payload: %w", err)
	}
	return nil
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if written < 0 || written > len(data) {
			return fmt.Errorf("%w: invalid writer count %d", ErrInvalidFrame, written)
		}
		data = data[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
