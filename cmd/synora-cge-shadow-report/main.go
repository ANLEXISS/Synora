package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"synora/internal/cge/shadowworkflow"
)

func main() {
	input := flag.String("input", "", "qualification samples NDJSON")
	manifestPath := flag.String("manifest", "", "qualification manifest JSON")
	output := flag.String("output", "", "qualification summary JSON")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: synora-cge-shadow-report --input samples.ndjson --manifest manifest.json --output summary.json")
		os.Exit(3)
	}
	manifest := *manifestPath
	if manifest == "" {
		manifest = filepath.Join(filepath.Dir(*input), "qualification.manifest.json")
	}
	samples, invalid, err := shadowworkflow.ReadQualificationSamples(*input, shadowworkflow.DefaultQualificationConfig().MaxSamples)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qualification report input unavailable")
		os.Exit(3)
	}
	manifestValue, err := shadowworkflow.ReadQualificationManifest(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qualification report manifest invalid")
		os.Exit(3)
	}
	report := shadowworkflow.BuildQualificationReport(samples, invalid, manifestValue, shadowworkflow.DefaultQualificationConfig().Thresholds)
	if err := writeReport(*output, report); err != nil {
		fmt.Fprintln(os.Stderr, "qualification report output unavailable")
		os.Exit(3)
	}
	fmt.Fprintf(os.Stderr, "qualification report status=%s samples=%d invalid=%d\n", report.OverallStatus, report.SamplesRead, report.SamplesInvalid)
	os.Exit(shadowworkflow.QualificationReportExitCode(report.OverallStatus))
}

func writeReport(path string, report shadowworkflow.QualificationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".qualification-report-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = directoryFile.Sync()
	_ = directoryFile.Close()
	return err
}
