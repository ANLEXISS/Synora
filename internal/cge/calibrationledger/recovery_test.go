package calibrationledger

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrailingRecordDetectionAndExplicitRepair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.ndjson")
	p := DefaultPolicy()
	s, err := OpenFileStore(path, p)
	if err != nil {
		t.Fatal(err)
	}
	first := testRecord(t, "tail-a", nil)
	second := testRecord(t, "tail-b", &first)
	_, _ = s.Append(context.Background(), first)
	_, _ = s.Append(context.Background(), second)
	_ = s.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 2 {
		t.Fatal("short ledger")
	}
	if err := os.WriteFile(path, data[:len(data)-1], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileStore(path, p); !errors.Is(err, ErrTrailingRecordTruncated) {
		t.Fatalf("truncated err=%v", err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(data[:len(data)-1]) {
		t.Fatal("trailing recovery changed the file while repair was disabled")
	}
	repair := p
	repair.RepairTrailingRecord = true
	fixed, err := OpenFileStore(path, repair)
	if err != nil {
		t.Fatal(err)
	}
	defer fixed.Close()
	if got := fixed.Snapshot(); got.RecordCount != 1 || got.LastSequence != 1 {
		t.Fatalf("fixed=%+v", got)
	}
}

func TestMidFileCorruptionAndForgedHashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.ndjson")
	s, err := OpenFileStore(path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	r := testRecord(t, "corruption", nil)
	if _, err = s.Append(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	data, _ := os.ReadFile(path)
	data[10] = 'X'
	_ = os.WriteFile(path, data, 0600)
	if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrMidFileCorruption) {
		t.Fatalf("corruption err=%v", err)
	}
}

func TestUnknownSchemaSequenceGapAndFingerprintForgery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.ndjson")
	s, err := OpenFileStore(path, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	r := testRecord(t, "forged", nil)
	_, _ = s.Append(context.Background(), r)
	_ = s.Close()
	data, _ := os.ReadFile(path)
	var env JournalEnvelope
	if err := json.Unmarshal(data[:len(data)-1], &env); err != nil {
		t.Fatal(err)
	}
	env.Record.RecordFingerprint = "forged"
	rewritten, _ := canonicalJSON(env)
	_ = os.WriteFile(path, append(rewritten, '\n'), 0600)
	if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrRecordFingerprintMismatch) {
		t.Fatalf("fingerprint err=%v", err)
	}
	_ = os.Remove(path)
	unknown := `{"schema_version":"unknown","sequence":1,"previous_envelope_hash":"x"}` + "\n"
	_ = os.WriteFile(path, []byte(unknown), 0600)
	if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("schema err=%v", err)
	}
}

func TestRecoveryContinuityGenesisAndSizeErrors(t *testing.T) {
	makeLedger := func(t *testing.T) (string, []byte) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "ledger.ndjson")
		s, err := OpenFileStore(path, DefaultPolicy())
		if err != nil {
			t.Fatal(err)
		}
		first := testRecord(t, "continuity-a", nil)
		second := testRecord(t, "continuity-b", &first)
		if _, err := s.Append(context.Background(), first); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Append(context.Background(), second); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return path, data
	}

	writeMutation := func(t *testing.T, path string, data []byte, mutate func(*JournalEnvelope)) {
		t.Helper()
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		var envelope JournalEnvelope
		if err := json.Unmarshal([]byte(lines[1]), &envelope); err != nil {
			t.Fatal(err)
		}
		mutate(&envelope)
		encoded, err := canonicalJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append([]byte(lines[0]+"\n"), append(encoded, '\n')...), 0600); err != nil {
			t.Fatal(err)
		}
	}

	resign := func(envelope *JournalEnvelope) {
		envelope.Record.RecordFingerprint = recordFingerprint(envelope.Record)
		envelope.RecordHash = recordHash(envelope.Record)
		envelope.EnvelopeHash = envelopeFingerprint(*envelope)
	}

	t.Run("duplicate sequence", func(t *testing.T) {
		path, data := makeLedger(t)
		writeMutation(t, path, data, func(envelope *JournalEnvelope) {
			envelope.Sequence = 1
			envelope.Record.Sequence = 1
			resign(envelope)
		})
		if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrDuplicateSequence) {
			t.Fatalf("duplicate sequence err=%v", err)
		}
	})

	t.Run("sequence gap", func(t *testing.T) {
		path, data := makeLedger(t)
		writeMutation(t, path, data, func(envelope *JournalEnvelope) {
			envelope.Sequence = 3
			envelope.Record.Sequence = 3
			resign(envelope)
		})
		if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrSequenceGap) {
			t.Fatalf("sequence gap err=%v", err)
		}
	})

	t.Run("hash chain mismatch", func(t *testing.T) {
		path, data := makeLedger(t)
		writeMutation(t, path, data, func(envelope *JournalEnvelope) {
			envelope.PreviousEnvelopeHash = "forged-previous-envelope"
		})
		if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrHashChainMismatch) {
			t.Fatalf("hash chain err=%v", err)
		}
	})

	t.Run("forged envelope fingerprint", func(t *testing.T) {
		path, data := makeLedger(t)
		writeMutation(t, path, data, func(envelope *JournalEnvelope) {
			envelope.EnvelopeHash = "forged-envelope"
		})
		if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrEnvelopeFingerprintMismatch) {
			t.Fatalf("envelope fingerprint err=%v", err)
		}
	})

	t.Run("invalid genesis", func(t *testing.T) {
		path, data := makeLedger(t)
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		var envelope JournalEnvelope
		if err := json.Unmarshal([]byte(lines[0]), &envelope); err != nil {
			t.Fatal(err)
		}
		envelope.PreviousEnvelopeHash = "forged-genesis"
		encoded, err := canonicalJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenFileStore(path, DefaultPolicy()); !errors.Is(err, ErrInvalidGenesis) {
			t.Fatalf("invalid genesis err=%v", err)
		}
	})

	t.Run("record too large", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ledger.ndjson")
		policy := DefaultPolicy()
		data := append(make([]byte, policy.MaxRecordBytes), '\n')
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenFileStore(path, policy); !errors.Is(err, ErrRecordTooLarge) {
			t.Fatalf("record too large err=%v", err)
		}
	})
}
