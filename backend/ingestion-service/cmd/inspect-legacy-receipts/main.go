// inspect-legacy-receipts inventories a bounded, externally supplied evidence
// file. It never connects to a database or manufactures missing source evidence.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/google/uuid"
)

const maxFileBytes = 256 << 20

type finding struct {
	Line int    `json:"line"`
	Code string `json:"code"`
}
type inventory struct {
	Version          int       `json:"version"`
	Snapshot         string    `json:"snapshot"`
	SHA256           string    `json:"sha256"`
	Tenant           string    `json:"tenant"`
	Table            string    `json:"table"`
	Object           string    `json:"object"`
	Rows             int       `json:"rows"`
	Valid            int       `json:"valid_evidence"`
	RawInput         int       `json:"with_raw_input"`
	HistoricalSchema int       `json:"with_historical_schema"`
	Mode             int       `json:"with_mode"`
	Audits           int       `json:"with_audits"`
	Results          int       `json:"with_results"`
	Findings         []finding `json:"findings"`
}

// inspect reads all bytes of one bounded export chunk. Nothing is emitted until
// its expected checksum and full parse are verified. Coverage denotes presence,
// not provenance or validity. Findings use line numbers, never keys or payloads.
func inspect(r io.Reader, expected string, result inventory, limit, rows int) (inventory, error) {
	h := sha256.New()
	bounded := &io.LimitedReader{R: r, N: maxFileBytes + 1}
	scan := bufio.NewScanner(io.TeeReader(bounded, h))
	scan.Buffer(make([]byte, 4096), (16<<20)+1)
	seen := make(map[string]bool)
	result.Findings = []finding{}
	for scan.Scan() {
		result.Rows++
		if result.Rows > rows {
			return inventory{}, errors.New("record limit exceeded; split at trusted export boundary")
		}
		line := scan.Bytes()
		var in store.LegacyImport
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var trailing any
		code := ""
		if !utf8.Valid(line) || decoder.Decode(&in) != nil || decoder.Decode(&trailing) != io.EOF {
			code = "invalid_envelope"
		} else {
			if len(in.RawJSON) > 0 {
				result.RawInput++
			}
			modelTable, exists := in.HistoricalModel.Tables[result.Object]
			if exists && in.HistoricalModel.RevisionID != "" {
				result.HistoricalSchema++
			}
			if in.Mode != "" {
				result.Mode++
			}
			if len(in.Audits) > 0 {
				result.Audits++
			}
			if len(in.Receipt.ResponsePayload) > 0 {
				result.Results++
			}
			switch {
			case in.Receipt.TenantID != result.Tenant || in.HistoricalModel.TenantID.String() != result.Tenant || !exists || modelTable.ID.String() != result.Table || in.ObjectType != result.Object:
				code = "scope_mismatch"
			case len(in.Receipt.Key) == 0 || len(in.Receipt.Key) > 1024:
				code = "invalid_key_bound"
			default:
				if seen[in.Receipt.Key] {
					code = "duplicate_receipt_key"
				} else {
					seen[in.Receipt.Key] = true
					err := store.ValidateLegacyEvidence(in, limit)
					if errors.Is(err, store.ErrLegacyEvidenceLimit) {
						code = "evidence_limit"
					} else if err != nil {
						code = "evidence_rejected"
					}
				}
			}
		}
		if code != "" {
			result.Findings = append(result.Findings, finding{result.Rows, code})
		} else {
			result.Valid++
		}
	}
	if scan.Err() != nil {
		return inventory{}, errors.New("input read or line bound failed")
	}
	if bounded.N == 0 {
		return inventory{}, errors.New("input byte limit exceeded")
	}
	result.SHA256 = hex.EncodeToString(h.Sum(nil))
	if result.SHA256 != expected {
		return inventory{}, errors.New("source checksum mismatch")
	}
	if result.Rows == 0 {
		return inventory{}, errors.New("empty source evidence")
	}
	return result, nil
}

func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("inspect-legacy-receipts", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("input", "", "trusted JSONL evidence file")
	snapshot := f.String("snapshot", "", "externally assigned snapshot identifier")
	digest := f.String("sha256", "", "expected file SHA-256 from trusted source manifest")
	tenant := f.String("tenant", "", "tenant UUID")
	table := f.String("table", "", "stable table UUID")
	object := f.String("object-type", "", "historical object type")
	limit := f.Int("command-bytes", 1<<20, "target evidence bound")
	rows := f.Int("max-records", 1000, "maximum records, 1..10000")
	if err := f.Parse(args); err != nil {
		return errors.New("invalid arguments")
	}
	t, e1 := uuid.Parse(*tenant)
	b, e2 := uuid.Parse(*table)
	d, e3 := hex.DecodeString(*digest)
	if f.NArg() != 0 || *path == "" || *snapshot == "" || len(*snapshot) > 256 || !utf8.ValidString(*snapshot) || strings.ContainsAny(*snapshot, "\r\n\x00") || e1 != nil || e2 != nil || t == uuid.Nil || b == uuid.Nil || e3 != nil || len(d) != 32 || *object == "" || len(*object) > 1024 || !utf8.ValidString(*object) || *limit < 1 || *limit > 16<<20 || *rows < 1 || *rows > 10000 {
		return errors.New("bounded input, snapshot, checksum and exact scope required")
	}
	file, err := os.Open(*path)
	if err != nil {
		return errors.New("cannot open evidence file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return errors.New("bounded regular evidence file required")
	}
	result, err := inspect(file, strings.ToLower(*digest), inventory{Version: 1, Snapshot: *snapshot, Tenant: t.String(), Table: b.String(), Object: *object}, *limit, *rows)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(out).Encode(result); err != nil {
		return err
	}
	if len(result.Findings) > 0 {
		return errors.New("source evidence exceptions require review")
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
