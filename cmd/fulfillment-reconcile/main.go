package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/model"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/reconcile"
	"github.com/martinlofranodeoliveira/fulfillment-reconciliation-lab/pkg/strictjson"
)

func main() {
	if err := runCommand(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func printUsage(w io.Writer) error {
	_, err := fmt.Fprint(w, "Fulfillment Reconciliation Lab 0.1.0 (development)\nUsage:\n  fulfillment-reconcile reconcile [--orders=file] [--reservations=file] [--shipments=file] [--format=text|json] [--out=new-file]\n  fulfillment-reconcile demo [--format=text|json]\nOmitted sources are unavailable; supply at least one file to reconcile.\n")
	if err != nil {
		return errors.New("output write failed")
	}
	return nil
}

func runCommand(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing command; use help")
	}
	switch args[0] {
	case "help", "--help", "-h":
		if len(args) != 1 {
			return errors.New("invalid arguments")
		}
		return printUsage(w)
	case "reconcile":
		return runReconcile(args[1:], w)
	case "demo":
		fs := flag.NewFlagSet("demo", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		format := fs.String("format", "text", "text or json")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return printUsage(w)
			}
			return errors.New("invalid arguments")
		}
		if fs.NArg() != 0 || (*format != "text" && *format != "json") {
			return errors.New("invalid arguments")
		}
		return runDemoFormat(w, *format)
	default:
		return errors.New("invalid command; use help")
	}
}

// The CLI targets Linux/POSIX. Nonblocking, no-follow open plus identity checks
// prevent an explicit FIFO/symlink or a replacement race from blocking a read.
func readSnapshotFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > model.MaxPayloadBytes {
		return nil, errors.New("input must be an accessible bounded regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("input file open failed")
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() > model.MaxPayloadBytes {
		_ = f.Close()
		return nil, errors.New("input file changed or is not a bounded regular file")
	}
	data, readErr := strictjson.ReadBounded(f)
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, errors.New("input file close failed")
	}
	return data, nil
}

func runReconcile(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ordersPath := fs.String("orders", "", "orders file")
	reservationsPath := fs.String("reservations", "", "reservations file")
	shipmentsPath := fs.String("shipments", "", "shipments file")
	format := fs.String("format", "text", "text or json")
	outPath := fs.String("out", "", "new output file")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printUsage(w)
		}
		return errors.New("invalid arguments")
	}
	if fs.NArg() != 0 || (*format != "text" && *format != "json") || (*ordersPath == "" && *reservationsPath == "" && *shipmentsPath == "") {
		return errors.New("invalid arguments")
	}
	var orders *model.OrderSnapshot
	var reservations *model.ReservationSnapshot
	var shipments *model.ShipmentSnapshot
	if *ordersPath != "" {
		data, err := readSnapshotFile(*ordersPath)
		if err != nil {
			return err
		}
		orders, err = strictjson.DecodeOrderSnapshot(bytes.NewReader(data))
		if err != nil {
			return err
		}
	}
	if *reservationsPath != "" {
		data, err := readSnapshotFile(*reservationsPath)
		if err != nil {
			return err
		}
		reservations, err = strictjson.DecodeReservationSnapshot(bytes.NewReader(data))
		if err != nil {
			return err
		}
	}
	if *shipmentsPath != "" {
		data, err := readSnapshotFile(*shipmentsPath)
		if err != nil {
			return err
		}
		shipments, err = strictjson.DecodeShipmentSnapshot(bytes.NewReader(data))
		if err != nil {
			return err
		}
	}
	report, err := reconcile.Reconcile(orders, reservations, shipments)
	if err != nil {
		return err
	}
	data, err := render(*format, report, func(out io.Writer) error { return formatTextReport(out, report) })
	if err != nil {
		return err
	}
	if *outPath == "" {
		return writeOutput(w, data)
	}
	// Exclusive creation cannot truncate an input, existing report or symlink.
	f, err := os.OpenFile(*outPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("output must be a new accessible file")
	}
	writeErr := writeOutput(f, data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return errors.New("output file close failed")
	}
	return nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > model.MaxReportBytes-b.Len() {
		return 0, errors.New("rendered report exceeds maximum")
	}
	return b.Buffer.Write(p)
}
func render(format string, value any, text func(io.Writer) error) ([]byte, error) {
	var b boundedBuffer
	var err error
	if format == "json" {
		encoder := json.NewEncoder(&b)
		encoder.SetIndent("", "  ")
		err = encoder.Encode(value)
	} else if format == "text" {
		err = text(&b)
	} else {
		return nil, errors.New("invalid output format")
	}
	if err != nil {
		return nil, errors.New("report rendering failed")
	}
	return b.Bytes(), nil
}
func writeOutput(w io.Writer, data []byte) error {
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		return errors.New("output write failed")
	}
	return nil
}

func formatTextReport(w io.Writer, rep *model.ReconciliationReport) error {
	if _, err := fmt.Fprintf(w, "Fulfillment Reconciliation Lab %s (development)\ncutoffs_aligned=%t pairs=%d matched=%d inconsistent=%d ambiguous=%d incomplete=%d orphan=%d\nORDER/SKU CLASSIFICATION requested reserved shipped remaining\n", rep.ReportVersion, rep.CutoffsAligned, rep.Summary.TotalOrderSKUPairs, rep.Summary.Matched, rep.Summary.Inconsistent, rep.Summary.Ambiguous, rep.Summary.Incomplete, rep.Summary.Orphan); err != nil {
		return err
	}
	for _, finding := range rep.Findings {
		if _, err := fmt.Fprintf(w, "%s/%s %s %d %d %d %d [%s]\n", finding.OrderID, finding.SKUID, finding.Classification, finding.RequestedUnits, finding.ReservedUnits, finding.ShippedUnits, finding.DifferenceRequestedVsShipped, strings.Join(finding.ReasonCodes, ",")); err != nil {
			return err
		}
		if finding.QuarantineNote != "" {
			if _, err := fmt.Fprintln(w, "  "+finding.QuarantineNote); err != nil {
				return err
			}
		}
	}
	if len(rep.SystemWarnings) > 0 {
		if _, err := fmt.Fprintln(w, "warnings="+strings.Join(rep.SystemWarnings, ",")); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w, rep.Disclaimer)
	return err
}
