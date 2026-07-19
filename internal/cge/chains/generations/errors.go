package generations

import "errors"

var (
	ErrInvalidGenerationStore     = errors.New("invalid_generation_store")
	ErrInvalidGenerationID        = errors.New("invalid_generation_id")
	ErrInvalidGenerationPath      = errors.New("invalid_generation_path")
	ErrGenerationAlreadyExists    = errors.New("generation_already_exists")
	ErrGenerationNotFound         = errors.New("generation_not_found")
	ErrGenerationWriteFailed      = errors.New("generation_write_failed")
	ErrGenerationMetadataMismatch = errors.New("generation_metadata_mismatch")
	ErrManifestNotFound           = errors.New("manifest_not_found")
	ErrManifestInvalid            = errors.New("manifest_invalid")
	ErrManifestSchemaUnsupported  = errors.New("manifest_schema_unsupported")
	ErrManifestWriteFailed        = errors.New("manifest_write_failed")
	ErrManifestGenerationMismatch = errors.New("manifest_generation_mismatch")
	ErrCheckpointMismatch         = errors.New("checkpoint_mismatch")
	ErrCheckpointNotFound         = errors.New("checkpoint_not_found")
	ErrInvalidContext             = errors.New("invalid_context")
	ErrInvalidTimestamp           = errors.New("invalid_timestamp")
)
