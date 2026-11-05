package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
)

func TestDemoActualE2EAndDeterministicCompleteOutput(t *testing.T) {
	var first, second bytes.Buffer
	if err := runCommand([]string{"demo", "--format=json"}, &first); err != nil {
		t.Fatal(err)
	}
	if err := runCommand([]string{"demo", "--format=json"}, &second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatal("complete demo JSON differs between runs")
	}
	var result demoResult
	if err := json.Unmarshal(first.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Scenarios) != 3 || result.Requests["orders"] != 1 || result.Requests["reservations"] != 1 || result.Requests["shipments"] != 2 {
		t.Fatal("demo did not perform expected actual HTTP interactions")
	}
	baseline := result.Scenarios[0].Report
	if baseline.Summary != (model.ReportSummary{TotalOrderSKUPairs: 4, Matched: 1, Ambiguous: 1, Inconsistent: 1, Orphan: 1}) {
		t.Fatal("demo classification oracle failed")
	}
	if baseline.Findings[0].ReservedUnits != 5 || baseline.Findings[0].ShippedUnits != 5 || baseline.Findings[1].ReservedUnits != 0 || baseline.Findings[2].DifferenceRequestedVsShipped != 2 {
		t.Fatal("demo numeric oracle failed")
	}
	if result.Scenarios[1].Report.Summary.Incomplete != 4 || result.Scenarios[2].Report.Summary.Incomplete != 3 || result.Failure != "server returned unexpected status code 500" {
		t.Fatal("demo missing-evidence or failure oracle failed")
	}
	var text bytes.Buffer
	if err := runDemo(&text); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"o1/s1 MATCHED 5 5 5 0", "o2/s2 AMBIGUOUS_EVIDENCE 5 0 5 0", "o3/s3 INCONSISTENT_QUANTITIES 5 5 3 2", "o999/s999 ORPHAN_EVIDENCE 0 0 2 -2", "Demo assertions verified; fixtures closed."} {
		if !strings.Contains(text.String(), expected) {
			t.Fatalf("text demo missing %q", expected)
		}
	}
}

func fixtureArgs() []string {
	return []string{"--orders=../../testdata/fixtures/orders_aligned.json", "--reservations=../../testdata/fixtures/reservations_aligned.json", "--shipments=../../testdata/fixtures/shipments_aligned.json", "--format=json"}
}
func TestFileCLIAndUnavailableSource(t *testing.T) {
	var output bytes.Buffer
	if err := runReconcile(fixtureArgs(), &output); err != nil {
		t.Fatal(err)
	}
	var report model.ReconciliationReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Matched != 3 || report.Summary.TotalOrderSKUPairs != 3 || report.Findings[0].ShippedUnits != 5 {
		t.Fatal("aligned committed fixture oracle failed")
	}
	output.Reset()
	if err := runReconcile([]string{"--orders=../../testdata/fixtures/orders_aligned.json", "--format=json"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Incomplete != 3 || report.Summary.Matched != 0 {
		t.Fatal("omitted file did not mean unavailable source")
	}
}

func TestCLIRejectsNonregularAndSanitizesDiagnostics(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "private-fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "private-link")
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(dir, "private-oversized")
	if err := os.WriteFile(oversized, []byte(strings.Repeat(" ", model.MaxPayloadBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(dir, "private-invalid")
	if err := os.WriteFile(invalid, []byte(`{"private-key":"private-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, fifo, link, oversized, invalid, filepath.Join(dir, "private-missing")} {
		var output bytes.Buffer
		err := runReconcile([]string{"--orders=" + path}, &output)
		if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), dir) || output.Len() != 0 {
			t.Fatalf("unsafe file acceptance or diagnostic: %v", err)
		}
	}
	for _, args := range [][]string{{}, {"--private-flag=private-value"}, {"--format=private-format", "--orders=private-path"}, {"--orders=private-path", "private-positional"}} {
		if err := runReconcile(args, &bytes.Buffer{}); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("CLI argument error leaked raw arguments")
		}
	}
	if err := runCommand([]string{"private-command"}, &bytes.Buffer{}); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("command diagnostic leaked input")
	}
}

func TestOutputNeverOverwritesCallerData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing")
	original := []byte("preserve caller data")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	args := append(fixtureArgs(), "--out="+path)
	if err := runReconcile(args, &bytes.Buffer{}); err == nil {
		t.Fatal("existing output was overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("caller file changed")
	}
	newPath := filepath.Join(dir, "new-report")
	if err := runReconcile(append(fixtureArgs(), "--out="+newPath), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(newPath)
	if err != nil || !json.Valid(data) {
		t.Fatal("new report not written")
	}
	info, err := os.Stat(newPath)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("report permissions are too broad")
	}
	// A report target that is also an input must not be truncated.
	input := filepath.Join(dir, "input.json")
	fixture, err := os.ReadFile("../../testdata/fixtures/orders_aligned.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	if err := runReconcile([]string{"--orders=" + input, "--out=" + input}, &bytes.Buffer{}); err == nil {
		t.Fatal("input/output alias accepted")
	}
	data, err = os.ReadFile(input)
	if err != nil || !bytes.Equal(data, fixture) {
		t.Fatal("input file damaged")
	}
}

type badWriter struct{}

func (badWriter) Write([]byte) (int, error) { return 0, errors.New("private-writer-failure") }
func TestOutputFailureAndRenderedBound(t *testing.T) {
	if err := runReconcile(fixtureArgs(), badWriter{}); err == nil || err.Error() != "output write failed" {
		t.Fatal("writer error not propagated safely")
	}
	var b boundedBuffer
	if _, err := b.Write(make([]byte, model.MaxReportBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("rendered byte limit bypassed")
	}
}
