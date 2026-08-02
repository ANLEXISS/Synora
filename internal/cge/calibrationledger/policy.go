package calibrationledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Policy struct {
	MaxDimensionsPerRecord             int    `json:"max_dimensions_per_record"`
	MaxRecordBytes                     int    `json:"max_record_bytes"`
	MaxLedgerBytes                     int64  `json:"max_ledger_bytes"`
	MaxRecords                         uint64 `json:"max_records"`
	Fsync                              bool   `json:"fsync"`
	RepairTrailingRecord               bool   `json:"repair_trailing_record"`
	StoreIncomparable                  bool   `json:"store_incomparable"`
	StoreStale                         bool   `json:"store_stale"`
	StoreInvalidated                   bool   `json:"store_invalidated"`
	DeduplicateByComparisonFingerprint bool   `json:"deduplicate_by_comparison_fingerprint"`
}

func DefaultPolicy() Policy {
	return Policy{MaxDimensionsPerRecord: 16, MaxRecordBytes: 64 * 1024, MaxLedgerBytes: 1 << 30, MaxRecords: 5_000_000, Fsync: true, StoreIncomparable: true, StoreStale: true, StoreInvalidated: true, DeduplicateByComparisonFingerprint: true}
}

func (p Policy) Validate() error {
	if p.MaxDimensionsPerRecord <= 0 || p.MaxDimensionsPerRecord > 64 || p.MaxRecordBytes <= 0 || p.MaxRecordBytes > 16<<20 || p.MaxLedgerBytes <= 0 || p.MaxRecords == 0 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Fingerprint() string {
	data, _ := json.Marshal(p)
	h := sha256.Sum256(data)
	return PolicySchemaVersion + ":" + hex.EncodeToString(h[:])
}
