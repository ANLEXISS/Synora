package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"synora/internal/eval"
)

func main() {
	fixturesPath := flag.String("fixtures", "Synora-Eval/fixtures/basic.jsonl", "fixture JSONL path")
	outputsPath := flag.String("outputs", "", "actual output JSONL path; empty runs the mock backend")
	reportPath := flag.String("report", "", "optional JSON report path")
	flag.Parse()

	fixtureData, err := os.ReadFile(*fixturesPath)
	if err != nil {
		fatal(err)
	}
	var report eval.Report
	if *outputsPath == "" {
		report, err = eval.RunMock(bytesReader(fixtureData))
	} else {
		outputData, readErr := os.ReadFile(*outputsPath)
		if readErr != nil {
			fatal(readErr)
		}
		report, err = eval.Run(bytesReader(fixtureData), bytesReader(outputData))
	}
	if err != nil {
		fatal(err)
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, append(body, '\n'), 0o640); err != nil {
			fatal(err)
		}
	} else {
		fmt.Println(string(body))
	}
	if report.Failed != 0 {
		os.Exit(1)
	}
}

func bytesReader(value []byte) *bytes.Reader { return bytes.NewReader(value) }

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
