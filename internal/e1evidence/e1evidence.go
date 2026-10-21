// Package e1evidence makes E1-a's evidence pack (docs/commercial-e1a.md
// 4.5, 4.7): Start records the chain before the runs; Pack writes the pack
// after them. Neither decides anything: verify-demo --mode e1a does.
package e1evidence

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/verification"
	_ "modernc.org/sqlite"
)

// PackInput is what Pack packs: the Host's Store database (read only), the
// start, the runs, the Host's trace directory ("" none), its reconciliation
// report ("" none) and the code the runs ran. Start.ToBlock is the pack's
// last block (E8 counts to it); 0 takes the reconciliation report's.
type PackInput struct {
	DB, Traces, Reconciliation, Commit string
	Start                              verification.E1aStart
	Tasks                              []verification.E1aTask
}

// Pack writes the pack into dst, a new directory, and returns its run ID.
func Pack(in PackInput, dst string) (string, error) {
	m := verification.E1aManifest{Version: verification.E1aVersion, CreatedAt: uint64(time.Now().Unix()), Commit: in.Commit, Tasks: in.Tasks, Start: in.Start}
	var id [3]byte
	rand.Read(id[:])
	m.RunID = "e1a-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(id[:])
	files, registry, err := export(in.DB, in.Tasks)
	if err != nil {
		return "", err
	}
	files["registry.json"] = registry
	for _, t := range in.Tasks {
		if in.Traces == "" {
			break
		}
		traces, err := tracesOf(in.Traces, t.TaskID)
		if err != nil {
			return "", err
		}
		for name, raw := range traces {
			files["traces/"+t.TaskID+"/"+name] = raw
		}
	}
	if in.Reconciliation != "" {
		raw, err := os.ReadFile(in.Reconciliation)
		if err != nil {
			return "", err
		}
		var rep struct {
			ToBlock uint64 `json:"to_block"`
		}
		if json.Unmarshal(raw, &rep) != nil {
			return "", errors.New("the reconciliation is not a report")
		}
		if m.Start.ToBlock == 0 {
			m.Start.ToBlock = rep.ToBlock
		}
		files["reconciliation.json"] = raw
	}
	if err := os.Mkdir(dst, 0o700); err != nil {
		return "", fmt.Errorf("the pack's directory must be new: %w", err)
	}
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	slices.Sort(names)
	for _, p := range names {
		if err := write(dst, p, files[p]); err != nil {
			return "", err
		}
		sum := sha256.Sum256(files[p])
		m.Files = append(m.Files, verification.E1aFile{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := write(dst, "manifest.json", raw); err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	end, _ := json.MarshalIndent(verification.E1aEnd{RunID: m.RunID, State: "complete", ManifestSHA256: hex.EncodeToString(sum[:])}, "", "  ")
	return m.RunID, write(dst, "run-end.json", end)
}

// Start records the chain before the runs (§4.7): the node's chain, the
// next block (E8 counts from it), the payer wallets, the token, payee and
// contracts of a host directory (p2-testnet e1a-host's), and refuses a task
// of its registry already registered on the budget contract.
func Start(ctx context.Context, chain verification.Chain, hostDir string) (verification.E1aStart, error) {
	var s verification.E1aStart
	var pay struct {
		Environment    string `json:"environment"`
		ChainID        uint64 `json:"chain_id"`
		Token          string `json:"token"`
		PayTo          string `json:"pay_to"`
		BudgetContract string `json:"budget_contract"`
	}
	var reg contracts.RegistryConfig
	if err := errors.Join(readJSON(filepath.Join(hostDir, "payments.json"), &pay), readJSON(filepath.Join(hostDir, "registry.json"), &reg)); err != nil {
		return s, err
	}
	s.Chain, s.ChainID, s.Token, s.PayTo, s.BudgetContract = pay.Environment, pay.ChainID, pay.Token, pay.PayTo, pay.BudgetContract
	if s.Chain == "" {
		s.Chain = "local_anvil"
	}
	id, err := chain.ChainID(ctx)
	if err != nil || id.Uint64() != s.ChainID {
		return s, fmt.Errorf("the node is not chain %d (%v, %v)", s.ChainID, id, err)
	}
	latest, err := chain.BlockNumber(ctx)
	if err != nil {
		return s, err
	}
	s.FromBlock = latest + 1
	budget, err := eth.ParseAddress(s.BudgetContract)
	if err != nil {
		return s, fmt.Errorf("budget_contract: %w", err)
	}
	v, err := chain.Call(ctx, eth.Msg{To: budget, Data: eth.Selector("verifier()")})
	if err != nil || len(v) != 32 {
		return s, fmt.Errorf("the budget contract's verifier: %v", err)
	}
	va, _ := eth.WordAddress(v)
	s.Verifier = va.Hex()
	for _, w := range reg.Wallets {
		s.Wallets = append(s.Wallets, w.From)
	}
	for _, t := range reg.Tasks {
		field, err := chain.Call(ctx, eth.Msg{To: budget, Data: mustCall("taskField(string)", t.TaskID)})
		if err != nil || len(field) != 32 {
			return s, fmt.Errorf("task %s: its field: %v", t.TaskID, err)
		}
		c, err := chain.Call(ctx, eth.Msg{To: budget, Data: mustCall("commitment(uint256)", new(big.Int).SetBytes(field))})
		if err != nil || len(c) != 32 {
			return s, fmt.Errorf("task %s: its commitment: %v", t.TaskID, err)
		}
		if new(big.Int).SetBytes(c).Sign() != 0 {
			return s, fmt.Errorf("task %s is registered on the budget contract already: give the run set new task IDs (registry-e1a.json)", t.TaskID)
		}
	}
	return s, nil
}

func mustCall(sig string, args ...any) []byte {
	b, _ := eth.Call(sig, args...)
	return b
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

// tracesOf is every trace of task in dir (TASK-<nanoseconds>.jsonl, as the
// buyer host names them; a retried run has more than one), by file name;
// none for a fake run.
func tracesOf(dir, task string) (map[string][]byte, error) {
	name := regexp.MustCompile(`^` + regexp.QuoteMeta(task) + `-[0-9]+\.jsonl$`)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !name.MatchString(e.Name()) {
			continue
		}
		if out[e.Name()], err = os.ReadFile(filepath.Join(dir, e.Name())); err != nil {
			return nil, err
		}
	}
	return out, nil
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
