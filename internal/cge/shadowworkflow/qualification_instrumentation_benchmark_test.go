package shadowworkflow

import (
	"encoding/json"
	"testing"
	"time"
)

func BenchmarkQualificationRecorderDisabled(b *testing.B) {
	cfg := DefaultQualificationConfig()
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		recorder, err := OpenQualificationRecorder(cfg, started, func() QualificationSample { return testQualificationSample() })
		if err != nil || recorder != nil {
			b.Fatalf("disabled recorder=%v err=%v", recorder, err)
		}
	}
}

func BenchmarkQualificationSampleFingerprint(b *testing.B) {
	sample := testQualificationSample()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = QualificationSampleFingerprint(sample)
	}
}

func BenchmarkQualificationSampleEncoding(b *testing.B) {
	sample := testQualificationSample()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(sample); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQualificationReport100Samples(b *testing.B) {
	manifest := QualificationManifest{SchemaVersion: "shadow-qualification-manifest-v1", RunID: "benchmark", Profile: QualificationProfileSmoke, StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ConfigurationFingerprint: "config", SampleInterval: time.Second, MaxSamples: 1000, MaxOutputBytes: 1 << 20, System: "local", GoVersion: "test"}
	manifest.Fingerprint = QualificationManifestFingerprint(manifest)
	samples := make([]QualificationSample, 100)
	for i := range samples {
		samples[i] = testQualificationSample()
		samples[i].SampleSequence = uint64(i + 1)
		samples[i].SampledAt = manifest.StartedAt.Add(time.Duration(i+1) * time.Second)
		samples[i].Uptime = time.Duration(i+1) * time.Second
		samples[i].Fingerprint = QualificationSampleFingerprint(samples[i])
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BuildQualificationReport(samples, 0, manifest, DefaultQualificationConfig().Thresholds)
	}
}

func BenchmarkQualificationMemoryTrend(b *testing.B) {
	manifest := QualificationManifest{SchemaVersion: "shadow-qualification-manifest-v1", RunID: "trend", Profile: QualificationProfileEndurance, StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ConfigurationFingerprint: "config", SampleInterval: time.Minute, MaxSamples: 1000, MaxOutputBytes: 1 << 20, System: "local", GoVersion: "test"}
	manifest.Fingerprint = QualificationManifestFingerprint(manifest)
	samples := make([]QualificationSample, 100)
	for i := range samples {
		samples[i] = testQualificationSample()
		samples[i].SampleSequence = uint64(i + 1)
		samples[i].SampledAt = manifest.StartedAt.Add(time.Duration(i+1) * time.Minute)
		samples[i].Uptime = time.Duration(i+1) * time.Minute
		samples[i].Process.RSSBytes = int64(1024 + i*8)
		samples[i].Fingerprint = QualificationSampleFingerprint(samples[i])
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = processReport(samples, time.Duration(len(samples))*time.Minute, time.Minute)
	}
}
