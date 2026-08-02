package durableworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Policy struct {
	MaxRecordBytes                        int
	MaxCheckpointBytes                    int
	MaxEpisodes                           int
	MaxAdvisoryRequestsPerEpisode         int
	MaxMappingsPerEpisode                 int
	MaxAuthorizationAssessmentsPerEpisode int
	SyncOnCommit                          bool
	AllowTruncatedFinalRecord             bool
	FileMode                              uint32
	DirectoryMode                         uint32
}

func DefaultPolicy() Policy {
	return Policy{MaxRecordBytes: 8 * 1024 * 1024, MaxCheckpointBytes: 256 * 1024 * 1024, MaxEpisodes: 10000, MaxAdvisoryRequestsPerEpisode: 256, MaxMappingsPerEpisode: 256, MaxAuthorizationAssessmentsPerEpisode: 256, SyncOnCommit: true, AllowTruncatedFinalRecord: true, FileMode: 0600, DirectoryMode: 0700}
}

func (p Policy) Validate() error {
	if p.MaxRecordBytes <= 0 || p.MaxCheckpointBytes <= 0 || p.MaxEpisodes <= 0 || p.MaxAdvisoryRequestsPerEpisode <= 0 || p.MaxMappingsPerEpisode <= 0 || p.MaxAuthorizationAssessmentsPerEpisode <= 0 || p.MaxRecordBytes > p.MaxCheckpointBytes {
		return ErrInvalidPolicy
	}
	if p.FileMode&0777 != 0600 || p.DirectoryMode&0777 != 0700 {
		return ErrInvalidFileMode
	}
	return nil
}

func (p Policy) Fingerprint() string {
	payload, _ := json.Marshal(p)
	digest := sha256.Sum256(payload)
	return "durable-workflow-policy-v1:" + hex.EncodeToString(digest[:])
}
