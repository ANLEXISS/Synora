package campaign

import (
	"context"
	"path/filepath"
	"testing"
)

func BenchmarkGenerateTimeline(b *testing.B) {
	profile, _ := ProfileByID("stable_single_resident_30d")
	profile.DurationDays = 7
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := GenerateTimeline(profile); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunCampaign1Day(b *testing.B) {
	profile, _ := ProfileByID("stable_single_resident_30d")
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		root := filepath.Join(b.TempDir(), "campaign")
		b.StartTimer()
		if _, err := Run(context.Background(), profile, RunOptions{RootDir: root, DaysOverride: 1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunCampaign7Days(b *testing.B) {
	profile, _ := ProfileByID("stable_single_resident_30d")
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		root := filepath.Join(b.TempDir(), "campaign")
		b.StartTimer()
		if _, err := Run(context.Background(), profile, RunOptions{RootDir: root, DaysOverride: 7}); err != nil {
			b.Fatal(err)
		}
	}
}
