package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/chains"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/contractcatalog"
)

var journalTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func TestBuildRecordUsesExactKindContract(t *testing.T) {
	kind, ok := contractcatalog.JournalKind(string(RecordKindChainAdded))
	if !ok {
		t.Fatal("chain.added kind is not catalogued")
	}
	if kind.Contract == "synora.cge.audit-record.v1" {
		t.Fatal("journal kind still uses the generic audit contract")
	}
	if _, err := buildRecord(1, RecordKindChainAdded, journalTestBase, "test", "correlation", GenesisPayload{}); err == nil {
		t.Fatal("wrong payload type from the same journal package was accepted")
	}
}

func journalMutation(at time.Time, actor, correlation, reason string) chains.MutationContext {
	return chains.MutationContext{At: at, Actor: actor, CorrelationID: correlation, Reason: reason}
}

func newTestJournal(t *testing.T, name string) *FileJournal {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".ndjson")
	j, err := NewFileJournal(path, FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new journal: %v", err)
	}
	return j
}

func initializeTestJournal(t *testing.T, name string) (*FileJournal, Record) {
	t.Helper()
	j := newTestJournal(t, name)
	record, err := j.Initialize(context.Background(), GenesisInput{
		JournalID: "journal-test", CreatedAt: journalTestBase, RecordedAt: journalTestBase,
		Purpose: "cge test audit", Actor: "test", CorrelationID: "genesis",
	})
	if err != nil {
		t.Fatalf("initialize journal: %v", err)
	}
	return j, record
}

func testChain(t *testing.T, id string) (*chains.Chain, chains.Snapshot, chains.RevisionRecord) {
	t.Helper()
	chain, err := chains.New(chains.ChainID(id), journalMutation(journalTestBase, "builder", "create-"+id, "create chain"))
	if err != nil {
		t.Fatalf("new chain: %v", err)
	}
	added := chain.Snapshot()
	if err := chain.SetStatus(chains.StatusActive, journalMutation(journalTestBase.Add(time.Second), "lifecycle", "transition-"+id, "activate chain")); err != nil {
		t.Fatalf("set status: %v", err)
	}
	history := chain.History()
	return chain, added, history[len(history)-1]
}

func appendChainAndTransition(t *testing.T, j *FileJournal, id string) (*chains.Chain, Record, Record) {
	t.Helper()
	chain, added, revision := testChain(t, id)
	addedRecord, err := j.AppendChainAdded(context.Background(), ChainAddedInput{
		Chain: added, RecordedAt: journalTestBase.Add(time.Second), Actor: "test", CorrelationID: "add-" + id,
	})
	if err != nil {
		t.Fatalf("append chain added: %v", err)
	}
	transition, err := j.AppendLifecycleTransition(context.Background(), LifecycleTransitionInput{
		ChainID: chains.ChainID(id), PreviousRevision: revision.PreviousRevision, NewRevision: revision.NewRevision,
		From: revision.PreviousStatus, To: revision.NewStatus, Revision: revision,
		RecordedAt: revision.At, Actor: revision.Actor, CorrelationID: revision.CorrelationID,
	})
	if err != nil {
		t.Fatalf("append lifecycle transition: %v", err)
	}
	return chain, addedRecord, transition
}

func TestGenesisAndInitializationRules(t *testing.T) {
	j, genesis := initializeTestJournal(t, "genesis")
	if genesis.Sequence != 1 || genesis.Kind != RecordKindGenesis || genesis.PreviousHash != GenesisPreviousHash || genesis.RecordHash == "" || genesis.PayloadSHA256 == "" {
		t.Fatalf("invalid genesis record: %#v", genesis)
	}
	if _, err := j.Initialize(context.Background(), GenesisInput{
		JournalID: "journal-test", CreatedAt: journalTestBase, RecordedAt: journalTestBase,
		Purpose: "cge test audit", Actor: "test", CorrelationID: "again",
	}); err == nil || !errors.Is(err, ErrJournalAlreadyInitialized) {
		t.Fatalf("expected double initialization error, got %v", err)
	}

	before := genesis
	read, err := j.ReadAll(context.Background())
	if err != nil || read.RecordCount != 1 || read.HeadHash != genesis.RecordHash {
		t.Fatalf("read genesis = %#v err=%v", read, err)
	}
	if !reflect.DeepEqual(before, read.Records[0]) {
		t.Fatalf("genesis changed after read: before=%#v after=%#v", before, read.Records[0])
	}

	pre := newTestJournal(t, "before-genesis")
	_, chain, _ := testChain(t, "before-genesis-chain")
	if _, err := pre.AppendChainAdded(context.Background(), ChainAddedInput{Chain: chain, RecordedAt: journalTestBase, Actor: "test", CorrelationID: "before"}); err == nil || !errors.Is(err, ErrJournalNotInitialized) {
		t.Fatalf("expected not initialized error, got %v", err)
	}
	if _, err := NewFileJournal(filepath.Join(t.TempDir(), "bad"), FileJournalOptions{FileMode: 0o666}); err == nil || !errors.Is(err, ErrInvalidFileMode) {
		t.Fatalf("expected invalid mode, got %v", err)
	}
	absent := newTestJournal(t, "absent")
	if _, err := absent.ReadAll(context.Background()); err == nil || !errors.Is(err, ErrJournalNotFound) {
		t.Fatalf("expected absent journal error, got %v", err)
	}
	invalidCases := []GenesisInput{
		{CreatedAt: journalTestBase, RecordedAt: journalTestBase, Purpose: "purpose", Actor: "test", CorrelationID: "corr"},
		{JournalID: "id", CreatedAt: journalTestBase, RecordedAt: journalTestBase.Add(time.Second), Purpose: "purpose", Actor: "test", CorrelationID: "corr"},
		{JournalID: "id", CreatedAt: journalTestBase, RecordedAt: journalTestBase, Purpose: "purpose", Actor: "", CorrelationID: "corr"},
		{JournalID: "id", CreatedAt: journalTestBase, RecordedAt: journalTestBase, Purpose: "purpose", Actor: "test", CorrelationID: "bad\ncorrelation"},
	}
	for index, input := range invalidCases {
		j := newTestJournal(t, "invalid-genesis-"+string(rune('a'+index)))
		if _, err := j.Initialize(context.Background(), input); err == nil {
			t.Fatalf("invalid genesis case %d was accepted", index)
		}
	}
}

func TestObservationAddedAppendUsesCompactValidatedDelta(t *testing.T) {
	j, _ := initializeTestJournal(t, "observation-added")
	chain, _, _ := appendChainAndTransition(t, j, "observation-chain")
	observation := chains.ObservationRef{ID: "observation-2", EventType: "vision.motion", Timestamp: journalTestBase.Add(3 * time.Second), NodeID: "hall"}
	frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: observation.ID, ObservedAt: observation.Timestamp, NodeID: observation.NodeID, Timezone: "UTC", Topology: cgecontext.TopologySnapshot{Revision: "journal-topology", CapturedAt: journalTestBase, Nodes: []cgecontext.Node{{ID: "hall", Kind: cgecontext.NodeCorridor}}}})
	if err != nil {
		t.Fatalf("resolve observation context: %v", err)
	}
	observation.Context = &frame
	mutation := chains.MutationContext{At: journalTestBase.Add(4 * time.Second), Actor: "observer", Reason: "explicit evidence", CorrelationID: "observation-2"}
	if err := chain.AddObservation(observation, mutation); err != nil {
		t.Fatalf("domain observation: %v", err)
	}
	revision := chain.History()[len(chain.History())-1]
	record, err := j.AppendObservationAdded(context.Background(), ObservationAddedInput{
		ChainID: chain.Snapshot().ID, PreviousRevision: revision.PreviousRevision, NewRevision: revision.NewRevision,
		Observation: observation, Revision: revision,
		RecordedAt: journalTestBase.Add(time.Hour), Actor: mutation.Actor, CorrelationID: mutation.CorrelationID,
	})
	if err != nil {
		t.Fatalf("append observation: %v", err)
	}
	if record.Kind != RecordKindObservationAdded || record.Sequence != 4 || record.RecordHash == "" || record.PayloadSHA256 == "" {
		t.Fatalf("unexpected observation record: %#v", record)
	}
	var payload ObservationAddedPayload
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		t.Fatalf("decode observation payload: %v", err)
	}
	if payload.ChainID != chain.Snapshot().ID || payload.PreviousRevision != revision.PreviousRevision || payload.NewRevision != revision.NewRevision || !reflect.DeepEqual(payload.Observation, observation) || !reflect.DeepEqual(payload.Revision, revision) {
		t.Fatalf("payload mismatch: payload=%#v revision=%#v", payload, revision)
	}
	read, err := j.ReadAll(context.Background())
	if err != nil || read.HeadSequence != record.Sequence || read.Records[3].Kind != RecordKindObservationAdded {
		t.Fatalf("read observation journal: snapshot=%#v err=%v", read, err)
	}
	invalid := ObservationAddedInput{ChainID: chain.Snapshot().ID, PreviousRevision: revision.PreviousRevision, NewRevision: revision.NewRevision + 1, Observation: observation, Revision: revision, RecordedAt: journalTestBase.Add(2 * time.Hour), Actor: mutation.Actor, CorrelationID: "invalid"}
	if _, err := j.AppendObservationAdded(context.Background(), invalid); err == nil || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("invalid observation delta was accepted: %v", err)
	}
}

func TestChainAddedAndLifecycleDeltaPreserveProvenance(t *testing.T) {
	j, _ := initializeTestJournal(t, "chain")
	chain, addedRecord, transitionRecord := appendChainAndTransition(t, j, "chain-a")

	snapshot, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if snapshot.RecordCount != 3 || snapshot.HeadSequence != 3 || snapshot.HeadHash != transitionRecord.RecordHash {
		t.Fatalf("unexpected journal head: %#v", snapshot)
	}
	var added ChainAddedPayload
	if err := json.Unmarshal(snapshot.Records[1].Payload, &added); err != nil {
		t.Fatalf("decode chain payload: %v", err)
	}
	restored, err := chains.Restore(added.Chain)
	if err != nil || !reflect.DeepEqual(added.Chain, restored.Snapshot()) {
		t.Fatalf("added chain is not restorable: chain=%#v err=%v restored=%#v", added.Chain, err, restored.Snapshot())
	}
	var lifecycle LifecycleTransitionPayload
	if err := json.Unmarshal(snapshot.Records[2].Payload, &lifecycle); err != nil {
		t.Fatalf("decode lifecycle payload: %v", err)
	}
	if lifecycle.ChainID != "chain-a" || lifecycle.PreviousRevision != 1 || lifecycle.NewRevision != 2 || lifecycle.From != chains.StatusCandidate || lifecycle.To != chains.StatusActive || lifecycle.Revision.Operation != chains.OperationStatusChanged {
		t.Fatalf("invalid lifecycle delta: %#v", lifecycle)
	}
	if addedRecord.Sequence != 2 || addedRecord.PreviousHash != snapshot.Records[0].RecordHash || transitionRecord.PreviousHash != addedRecord.RecordHash {
		t.Fatalf("invalid hash linkage: added=%#v transition=%#v", addedRecord, transitionRecord)
	}
	if chain.Snapshot().Status != chains.StatusActive || chain.Snapshot().Revision != 2 {
		t.Fatalf("journal append mutated source chain unexpectedly: %#v", chain.Snapshot())
	}

	copyOfRead := snapshot.Clone()
	copyOfRead.Records[1].Payload[0] ^= 1
	if bytes.Equal(copyOfRead.Records[1].Payload, snapshot.Records[1].Payload) {
		t.Fatal("journal snapshot clone shares payload bytes")
	}
}

func TestSnapshotCheckpointUsesPrecedingHead(t *testing.T) {
	j, _ := initializeTestJournal(t, "checkpoint")
	_, _, transition := appendChainAndTransition(t, j, "checkpoint-chain")
	input := SnapshotCheckpointInput{
		SnapshotSchemaVersion: 1, SnapshotCreatedAt: journalTestBase.Add(time.Hour), SnapshotChainCount: 1,
		SnapshotPayloadSHA256: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		SnapshotSizeBytes:     1234, JournalSequence: transition.Sequence, JournalHeadHash: transition.RecordHash,
		RecordedAt: journalTestBase.Add(time.Hour), Actor: "snapshotter", CorrelationID: "checkpoint-1",
	}
	record, err := j.AppendSnapshotCheckpoint(context.Background(), input)
	if err != nil {
		t.Fatalf("append checkpoint: %v", err)
	}
	if record.Sequence != transition.Sequence+1 {
		t.Fatalf("checkpoint sequence=%d", record.Sequence)
	}
	if _, err := j.AppendSnapshotCheckpoint(context.Background(), input); err == nil || !errors.Is(err, ErrInvalidCheckpoint) {
		t.Fatalf("expected stale checkpoint rejection, got %v", err)
	}
	if _, err := j.ReadAll(context.Background()); err != nil {
		t.Fatalf("read checkpoint journal: %v", err)
	}
}

func TestJournalReaderDetectsTamperingAndReordering(t *testing.T) {
	tests := []struct {
		name string
		edit func([][]byte)
		want error
	}{
		{"payload", func(lines [][]byte) {
			lines[1] = bytes.Replace(lines[1], []byte(`"chain"`), []byte(`"chain_changed"`), 1)
		}, ErrPayloadChecksumMismatch},
		{"record hash", func(lines [][]byte) {
			lines[1] = bytes.Replace(lines[1], []byte(`"record_hash":"sha256:`), []byte(`"record_hash":"sha256:0`), 1)
		}, ErrRecordHashMismatch},
		{"payload checksum", func(lines [][]byte) {
			lines[1] = bytes.Replace(lines[1], []byte(`"payload_sha256":"sha256:`), []byte(`"payload_sha256":"sha256:0`), 1)
		}, ErrPayloadChecksumMismatch},
		{"previous hash", func(lines [][]byte) {
			lines[2] = tamperPreviousHash(lines[2])
		}, ErrPreviousHashMismatch},
		{"delete line", func(lines [][]byte) { copy(lines[1:], lines[2:]); lines = lines[:len(lines)-1] }, ErrSequenceGap},
		{"swap lines", func(lines [][]byte) { lines[1], lines[2] = lines[2], lines[1] }, ErrSequenceGap},
		{"duplicate line", func(lines [][]byte) { lines[2] = append([]byte(nil), lines[1]...) }, ErrSequenceGap},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			j, _ := initializeTestJournal(t, "tamper-"+test.name)
			appendChainAndTransition(t, j, "tamper-chain")
			path := j.path
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read journal: %v", err)
			}
			lines := bytes.Split(data, []byte("\n"))
			lines = lines[:len(lines)-1]
			test.edit(lines)
			if test.name == "delete line" {
				lines = lines[:len(lines)-1]
			}
			if err := os.WriteFile(path, bytes.Join(append(lines, nil), []byte("\n")), 0o640); err != nil {
				t.Fatalf("write tampered journal: %v", err)
			}
			if _, err := j.ReadAll(context.Background()); err == nil || !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestJournalReaderRejectsMalformedFiles(t *testing.T) {
	j, _ := initializeTestJournal(t, "malformed")
	appendChainAndTransition(t, j, "malformed-chain")
	path := j.path
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read valid journal: %v", err)
	}
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrJournalEmpty},
		{"truncated", valid[:len(valid)-1], ErrInvalidRecord},
		{"empty middle line", bytes.Replace(valid, []byte("\n"), []byte("\n\n"), 1), ErrInvalidRecord},
		{"multiple objects one line", bytes.Replace(valid, []byte("\n"), []byte("{}\n"), 1), ErrInvalidRecord},
		{"unknown kind", bytes.Replace(valid, []byte(`"kind":"journal.genesis"`), []byte(`"kind":"unknown"`), 1), ErrInvalidRecordKind},
		{"unknown schema", bytes.Replace(valid, []byte(`"schema_version":1`), []byte(`"schema_version":99`), 1), ErrUnsupportedSchema},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(path, test.data, 0o640); err != nil {
				t.Fatalf("write malformed file: %v", err)
			}
			if _, err := j.ReadAll(context.Background()); err == nil || !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestJournalLimitsErrorsAndDefensiveRead(t *testing.T) {
	if _, err := NewFileJournal("", FileJournalOptions{}); err == nil || !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("expected invalid path, got %v", err)
	}
	j, _ := initializeTestJournal(t, "limits")
	if _, err := j.AppendChainAdded(context.Background(), ChainAddedInput{RecordedAt: journalTestBase, Actor: "test", CorrelationID: "bad"}); err == nil || !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("expected invalid chain payload, got %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := j.AppendChainAdded(cancelled, ChainAddedInput{RecordedAt: journalTestBase, Actor: "test", CorrelationID: "cancel"}); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled append, got %v", err)
	}
	read, err := j.ReadAll(context.Background())
	if err != nil || read.RecordCount != 1 {
		t.Fatalf("cancelled append changed journal: %#v err=%v", read, err)
	}
	read.Records[0].Payload[0] ^= 1
	again, err := j.ReadAll(context.Background())
	if err != nil || bytes.Equal(read.Records[0].Payload, again.Records[0].Payload) {
		t.Fatal("ReadAll did not return defensive records")
	}

	tooSmall, err := NewFileJournal(filepath.Join(t.TempDir(), "record-limit"), FileJournalOptions{MaxJournalSize: 4096, MaxRecordSize: 32, CreateParentDirs: true})
	if err != nil {
		t.Fatalf("new record-limited journal: %v", err)
	}
	if _, err := tooSmall.Initialize(context.Background(), GenesisInput{JournalID: "small", CreatedAt: journalTestBase, RecordedAt: journalTestBase, Purpose: "purpose", Actor: "test", CorrelationID: "small"}); err == nil || !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("expected record limit error, got %v", err)
	}
	if _, err := NewFileJournal(filepath.Join(t.TempDir(), "bad-limit"), FileJournalOptions{MaxJournalSize: 10, MaxRecordSize: 20}); err == nil || !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("expected invalid limit error, got %v", err)
	}
}

func TestJournalAppendConcurrentAndExternalModification(t *testing.T) {
	j, _ := initializeTestJournal(t, "concurrent")
	const count = 32
	var wait sync.WaitGroup
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			chain := chains.Snapshot{
				ID: chains.ChainID("concurrent-" + string(rune('a'+index))), Status: chains.StatusCandidate,
				MaxHistoricalConfidence: 0, Revision: 1,
				History: []chains.RevisionRecord{{ChainID: chains.ChainID("concurrent-" + string(rune('a'+index))), Operation: chains.OperationChainCreated, PreviousRevision: 0, NewRevision: 1, At: journalTestBase, Actor: "test", Reason: "create", NewStatus: chains.StatusCandidate}},
			}
			if _, err := j.AppendChainAdded(context.Background(), ChainAddedInput{Chain: chain, RecordedAt: journalTestBase.Add(time.Duration(index+1) * time.Second), Actor: "test", CorrelationID: "concurrent"}); err != nil {
				t.Errorf("concurrent append %d: %v", index, err)
			}
		}(i)
	}
	wait.Wait()
	read, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("read concurrent journal: %v", err)
	}
	if read.RecordCount != count+1 || read.HeadSequence != uint64(count+1) {
		t.Fatalf("concurrent journal count=%d head=%d", read.RecordCount, read.HeadSequence)
	}

	path := j.path
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Fatal("concurrent journal unexpectedly empty")
	}
	current, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("read current head: %v", err)
	}
	_, externalSnapshot, _ := testChain(t, "external")
	external, err := buildRecord(current.HeadSequence+1, RecordKindChainAdded, journalTestBase.Add(time.Hour), "external", "external", ChainAddedPayload{Chain: externalSnapshot})
	if err != nil {
		t.Fatalf("build external record: %v", err)
	}
	external.PreviousHash = current.HeadHash
	external.RecordHash = hashRecord(external)
	line, err := encodeRecordLine(external)
	if err != nil {
		t.Fatalf("encode external record: %v", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		t.Fatalf("open external append: %v", err)
	}
	_, _ = file.Write(line)
	_ = file.Sync()
	_ = file.Close()
	chain, _, _ := testChain(t, "after-external")
	if _, err := j.AppendChainAdded(context.Background(), ChainAddedInput{Chain: chain.Snapshot(), RecordedAt: journalTestBase.Add(time.Hour), Actor: "test", CorrelationID: "after"}); err == nil || !errors.Is(err, ErrExternalModificationDetected) {
		t.Fatalf("expected external modification rejection, got %v", err)
	}
}

func TestJournalDeterminism(t *testing.T) {
	j1, _ := initializeTestJournal(t, "deterministic-a")
	j2, _ := initializeTestJournal(t, "deterministic-b")
	chain1, added1, revision1 := testChain(t, "same")
	chain2, added2, revision2 := testChain(t, "same")
	for _, item := range []struct {
		j        *FileJournal
		chain    chains.Snapshot
		revision chains.RevisionRecord
	}{
		{j1, added1, revision1}, {j2, added2, revision2},
	} {
		if _, err := item.j.AppendChainAdded(context.Background(), ChainAddedInput{Chain: item.chain, RecordedAt: journalTestBase.Add(time.Second), Actor: "test", CorrelationID: "add"}); err != nil {
			t.Fatalf("deterministic chain append: %v", err)
		}
		if _, err := item.j.AppendLifecycleTransition(context.Background(), LifecycleTransitionInput{ChainID: "same", PreviousRevision: 1, NewRevision: 2, From: chains.StatusCandidate, To: chains.StatusActive, Revision: item.revision, RecordedAt: item.revision.At, Actor: item.revision.Actor, CorrelationID: item.revision.CorrelationID}); err != nil {
			t.Fatalf("deterministic transition append: %v", err)
		}
	}
	data1, _ := os.ReadFile(j1.path)
	data2, _ := os.ReadFile(j2.path)
	if !bytes.Equal(data1, data2) {
		t.Fatal("identical explicit inputs produced different NDJSON")
	}
	if chain1.Snapshot().Revision != chain2.Snapshot().Revision {
		t.Fatal("fixture chains differ")
	}
}

func tamperPreviousHash(line []byte) []byte {
	var record Record
	if err := json.Unmarshal(line, &record); err != nil {
		return line
	}
	record.PreviousHash = GenesisPreviousHash
	record.RecordHash = hashRecord(record)
	result, err := json.Marshal(record)
	if err != nil {
		return line
	}
	return result
}
