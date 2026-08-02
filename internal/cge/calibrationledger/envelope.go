package calibrationledger

func makeEnvelope(record CalibrationRecord, previous string) JournalEnvelope {
	e := JournalEnvelope{SchemaVersion: EnvelopeSchemaVersion, Sequence: record.Sequence, PreviousEnvelopeHash: previous, Record: record, RecordHash: recordHash(record)}
	e.EnvelopeHash = envelopeFingerprint(e)
	return e
}
