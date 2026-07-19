package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadHead reads and validates only the final record and file size. It is a
// bounded check for local append validation; it cannot detect an arbitrary
// edit to an older byte, which is why ReadAll remains mandatory at replay and
// recovery boundaries.
func (j *FileJournal) ReadHead(ctx context.Context) (JournalHead, error) {
	if err := checkContext(ctx); err != nil {
		return JournalHead{}, err
	}
	if err := j.validate(); err != nil {
		return JournalHead{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return readJournalHead(ctx, j.path, j.options.MaxRecordSize)
}

func readJournalHead(ctx context.Context, path string, maxRecordSize int) (JournalHead, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return JournalHead{}, ErrJournalNotFound
		}
		return JournalHead{}, fmt.Errorf("%w: stat journal: %v", ErrInvalidRecord, err)
	}
	if info.IsDir() || info.Size() == 0 {
		return JournalHead{}, ErrJournalEmpty
	}
	file, err := os.Open(path)
	if err != nil {
		return JournalHead{}, fmt.Errorf("%w: open journal: %v", ErrInvalidRecord, err)
	}
	defer file.Close()
	window := int64(4096)
	maxWindow := int64(maxRecordSize) + 1
	var data []byte
	var start int64
	var lineStart, end int
	for {
		if window > info.Size() {
			window = info.Size()
		}
		start = info.Size() - window
		data = make([]byte, window)
		if _, err := io.ReadFull(io.NewSectionReader(file, start, window), data); err != nil {
			return JournalHead{}, fmt.Errorf("%w: read journal head: %v", ErrInvalidRecord, err)
		}
		if err := checkContext(ctx); err != nil {
			return JournalHead{}, err
		}
		if data[len(data)-1] != '\n' {
			return JournalHead{}, fmt.Errorf("%w: final line has no newline", ErrInvalidRecord)
		}
		end = len(data) - 1
		lineStart = bytes.LastIndexByte(data[:end], '\n') + 1
		if lineStart > 0 || start == 0 {
			break
		}
		if window >= maxWindow {
			return JournalHead{}, fmt.Errorf("%w: final record exceeds bounded head window", ErrRecordTooLarge)
		}
		window *= 2
		if window > maxWindow {
			window = maxWindow
		}
	}
	var record Record
	if err := decodeStrictJSON(data[lineStart:end], &record); err != nil {
		return JournalHead{}, fmt.Errorf("%w: decode journal head: %v", ErrInvalidRecord, err)
	}
	if err := record.Validate(); err != nil {
		return JournalHead{}, err
	}
	if hashRecord(record) != record.RecordHash {
		return JournalHead{}, ErrRecordHashMismatch
	}
	return JournalHead{Sequence: record.Sequence, Hash: record.RecordHash, Size: info.Size()}, nil
}
