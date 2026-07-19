package journal

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestReadHeadMatchesValidatedJournal(t *testing.T) {
	j, genesis := initializeTestJournal(t, "head")
	path := j.path
	head, err := j.ReadHead(context.Background())
	if err != nil {
		t.Fatalf("read initial head: %v", err)
	}
	if head.Sequence != genesis.Sequence || head.Hash != genesis.RecordHash || head.Size <= 0 {
		t.Fatalf("initial head = %#v, genesis = %#v", head, genesis)
	}

	_, _, transition := appendChainAndTransition(t, j, "head-chain")
	read, err := j.ReadAll(context.Background())
	if err != nil {
		t.Fatalf("read complete journal: %v", err)
	}
	head, err = j.ReadHead(context.Background())
	if err != nil {
		t.Fatalf("read final head: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if head.Sequence != read.HeadSequence || head.Hash != read.HeadHash || head.Size != info.Size() || head.Hash != transition.RecordHash {
		t.Fatalf("head %#v does not match read %#v", head, read)
	}
}

func TestReadHeadDoesNotReplaceFullHistoricalValidation(t *testing.T) {
	j, _ := initializeTestJournal(t, "historical-corruption")
	_, _, _ = appendChainAndTransition(t, j, "historical-chain")
	path := j.path
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index := bytes.Index(data, []byte("cge test audit"))
	if index < 0 {
		t.Fatal("fixture text was not found")
	}
	data[index] = 'x'
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ReadHead(context.Background()); err != nil {
		t.Fatalf("tail head should remain readable after historical corruption: %v", err)
	}
	if _, err := j.ReadAll(context.Background()); err == nil {
		t.Fatal("full validation accepted historical corruption")
	}
}
