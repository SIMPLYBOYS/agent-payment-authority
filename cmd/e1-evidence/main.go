// Command e1-evidence makes E1-a's evidence pack (docs/commercial-e1a.md
// 4.5, 4.7):
//
//	e1-evidence pack --db HOST.db --start START.json --task TASK=ENTRY[=AE1CASE]... \
//	    [--traces RUNS] [--reconciliation REPORT.json] [--commit SHA] --out artifacts/e1a/<new>
//
// pack reads the Host's Store database read only: each task's events and
// purchase orders as stored, and its registry snapshot. It copies each live
// run's trace (already whitelisted by the buyer host) and the
// reconciliation report, records the start (START.json: the chain, the first
// block, the wallets and contracts, written before the runs), indexes every
// file with its sha256 and closes the pack with run-end.json. It writes a
// new directory only, and decides nothing: verify-demo --mode e1a does.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/verification"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "e1-evidence:", err)
		os.Exit(1)
	}
}

type taskFlags []verification.E1aTask

func (t *taskFlags) String() string { return fmt.Sprint(*t) }
func (t *taskFlags) Set(v string) error {
	p := strings.Split(v, "=")
	if len(p) < 2 || len(p) > 3 || p[0] == "" || p[1] == "" {
		return errors.New("TASK=ENTRY or TASK=ENTRY=AE1CASE")
	}
	x := verification.E1aTask{TaskID: p[0], Scenario: p[1]}
	if len(p) == 3 {
		x.InjectionCase = p[2]
	}
	*t = append(*t, x)
	return nil
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "pack" {
		return errors.New("usage: e1-evidence pack --db HOST.db --start START.json --task TASK=ENTRY[=AE1CASE]... [--traces DIR] [--reconciliation FILE] [--commit SHA] --out DIR")
	}
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	db := fs.String("db", "", "the Host's Store database (read only)")
	start := fs.String("start", "", "the start recorded before the runs")
	traces := fs.String("traces", "", "the Host's trace directory (live runs)")
	recon := fs.String("reconciliation", "", "the Host's reconciliation report")
	commit := fs.String("commit", "", "the code the runs ran")
	dst := fs.String("out", "", "a new directory")
	var tasks taskFlags
	fs.Var(&tasks, "task", "TASK=ENTRY[=AE1CASE], once per run")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *db == "" || *start == "" || *dst == "" || len(tasks) == 0 {
		return errors.New("pack: --db, --start, --out and at least one --task")
	}
	m := verification.E1aManifest{Version: verification.E1aVersion, CreatedAt: uint64(time.Now().Unix()), Commit: *commit, Tasks: tasks}
	if err := readJSON(*start, &m.Start); err != nil {
		return fmt.Errorf("--start: %w", err)
	}
	var id [3]byte
	rand.Read(id[:])
	m.RunID = "e1a-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(id[:])
	files, registry, err := export(*db, tasks)
	if err != nil {
		return err
	}
	files["registry.json"] = registry
	for _, t := range tasks {
		if *traces == "" {
			break
		}
		if raw, err := latestTrace(*traces, t.TaskID); err != nil {
			return err
		} else if raw != nil {
			files["traces/"+t.TaskID+".jsonl"] = raw
		}
	}
	if *recon != "" {
		raw, err := os.ReadFile(*recon)
		if err != nil {
			return err
		}
		var rep struct {
			ToBlock uint64 `json:"to_block"`
		}
		if json.Unmarshal(raw, &rep) != nil {
			return errors.New("--reconciliation is not a report")
		}
		m.Start.ToBlock = rep.ToBlock
		files["reconciliation.json"] = raw
	}
	if err := os.Mkdir(*dst, 0o700); err != nil {
		return fmt.Errorf("--out must be a new directory: %w", err)
	}
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	slices.Sort(names)
	for _, p := range names {
		if err := write(*dst, p, files[p]); err != nil {
			return err
		}
		sum := sha256.Sum256(files[p])
		m.Files = append(m.Files, verification.E1aFile{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := write(*dst, "manifest.json", raw); err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	end, _ := json.MarshalIndent(verification.E1aEnd{RunID: m.RunID, State: "complete", ManifestSHA256: hex.EncodeToString(sum[:])}, "", "  ")
	if err := write(*dst, "run-end.json", end); err != nil {
		return err
	}
	fmt.Fprintf(out, "e1-evidence: %s: %d files in %s; check it with go run ./cmd/verify-demo --mode e1a --run-dir %s\n", m.RunID, len(names)+2, *dst, *dst)
	return nil
}

// export is each task's events and purchase orders, as stored, and the
// registry its tasks were started under (one for the whole pack).
func export(path string, tasks []verification.E1aTask) (map[string][]byte, []byte, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	files := map[string][]byte{}
	var registry []byte
	for _, t := range tasks {
		var snap string
		if err := db.QueryRow(`SELECT s.body FROM tasks t JOIN config_snapshots s ON s.snapshot_hash = t.registry_snapshot WHERE t.task_id = ?`, t.TaskID).Scan(&snap); err != nil {
			return nil, nil, fmt.Errorf("task %s: %w", t.TaskID, err)
		}
		if registry != nil && string(registry) != snap {
			return nil, nil, fmt.Errorf("task %s was started under another registry", t.TaskID)
		}
		registry = []byte(snap)
		for name, q := range map[string]string{
			"events.json":    `SELECT body FROM events WHERE task_id = ? ORDER BY sequence`,
			"purchases.json": `SELECT body FROM purchases WHERE task_id = ? ORDER BY decided_at, purchase_order_id`,
		} {
			rows, err := db.Query(q, t.TaskID)
			if err != nil {
				return nil, nil, err
			}
			list := []json.RawMessage{}
			for rows.Next() {
				var body string
				if err := rows.Scan(&body); err != nil {
					rows.Close()
					return nil, nil, err
				}
				list = append(list, json.RawMessage(body))
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return nil, nil, err
			}
			files["tasks/"+t.TaskID+"/"+name], _ = json.MarshalIndent(list, "", "  ")
		}
	}
	return files, registry, nil
}

// latestTrace is task's newest trace in dir (TASK-<nanoseconds>.jsonl), or
// nil when it has none (a fake run keeps none).
func latestTrace(dir, task string) ([]byte, error) {
	m, err := filepath.Glob(filepath.Join(dir, task+"-*.jsonl"))
	if err != nil || len(m) == 0 {
		return nil, err
	}
	slices.Sort(m)
	return os.ReadFile(m[len(m)-1])
}

func write(dir, rel string, raw []byte) error {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, raw, 0o600)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
