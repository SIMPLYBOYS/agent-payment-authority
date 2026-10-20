package eth

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Client is a JSON-RPC client over HTTP for one chain.
type Client struct {
	URL  string
	HTTP *http.Client
	id   atomic.Int64
}

// NewClient talks to url with a bounded timeout and response size.
func NewClient(url string) *Client {
	return &Client{URL: url, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// RPCError is the node's own error answer.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Raw calls a node method the client has no wrapper for (eth_getCode,
// anvil's own), its result decoded into out (nil: discarded).
func (c *Client) Raw(ctx context.Context, method string, out any, params ...any) error {
	return c.call(ctx, method, out, params...)
}

func (c *Client) call(ctx context.Context, method string, out any, params ...any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.id.Add(1), "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: the node's URL does not parse", method) // not the URL itself
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		// Not the URL: a node's URL may carry an API key in its path or query.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("%s: %w", method, ue.Err)
		}
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("%s: HTTP %d, not JSON-RPC", method, res.StatusCode)
	}
	if r.Error != nil {
		return r.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(r.Result, out)
}

func hexUint(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok || !strings.HasPrefix(s, "0x") {
		return nil, fmt.Errorf("quantity %q", s)
	}
	return v, nil
}

func hexBytes(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "0x") {
		return nil, fmt.Errorf("data %q", s)
	}
	return hex.DecodeString(s[2:])
}

func quantity(ctx context.Context, c *Client, method string, params ...any) (*big.Int, error) {
	var s string
	if err := c.call(ctx, method, &s, params...); err != nil {
		return nil, err
	}
	return hexUint(s)
}

// ChainID is eth_chainId.
func (c *Client) ChainID(ctx context.Context) (*big.Int, error) {
	return quantity(ctx, c, "eth_chainId")
}

// BlockNumber is eth_blockNumber.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	n, err := quantity(ctx, c, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// PendingNonce is the account's next nonce (eth_getTransactionCount pending).
func (c *Client) PendingNonce(ctx context.Context, a Address) (uint64, error) {
	return c.nonceAt(ctx, a, "pending")
}

// NonceAt is the account's next nonce as of block n: every nonce below it
// is used by a transaction mined at or before n.
func (c *Client) NonceAt(ctx context.Context, a Address, n uint64) (uint64, error) {
	return c.nonceAt(ctx, a, fmt.Sprintf("0x%x", n))
}

func (c *Client) nonceAt(ctx context.Context, a Address, block string) (uint64, error) {
	n, err := quantity(ctx, c, "eth_getTransactionCount", a.Hex(), block)
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// Balance is eth_getBalance at latest.
func (c *Client) Balance(ctx context.Context, a Address) (*big.Int, error) {
	return quantity(ctx, c, "eth_getBalance", a.Hex(), "latest")
}

// Header is the part of a block P2 reads.
type Header struct {
	Number    uint64
	Timestamp uint64
	BaseFee   *big.Int
}

// LatestHeader is eth_getBlockByNumber("latest").
func (c *Client) LatestHeader(ctx context.Context) (Header, error) {
	return c.header(ctx, "latest")
}

// HeaderAt is block n's header.
func (c *Client) HeaderAt(ctx context.Context, n uint64) (Header, error) {
	return c.header(ctx, fmt.Sprintf("0x%x", n))
}

func (c *Client) header(ctx context.Context, tag string) (Header, error) {
	var b struct {
		Number, Timestamp, BaseFeePerGas string
	}
	if err := c.call(ctx, "eth_getBlockByNumber", &b, tag, false); err != nil {
		return Header{}, err
	}
	n, err1 := hexUint(b.Number)
	ts, err2 := hexUint(b.Timestamp)
	fee, err3 := hexUint(b.BaseFeePerGas)
	if err := errors.Join(err1, err2, err3); err != nil {
		return Header{}, fmt.Errorf("block: %w", err)
	}
	return Header{Number: n.Uint64(), Timestamp: ts.Uint64(), BaseFee: fee}, nil
}

// MaxPriorityFee is eth_maxPriorityFeePerGas.
func (c *Client) MaxPriorityFee(ctx context.Context) (*big.Int, error) {
	return quantity(ctx, c, "eth_maxPriorityFeePerGas")
}

// Msg is a call or a gas estimate's message.
type Msg struct {
	From  Address
	To    Address
	Data  []byte
	Value *big.Int
}

func (m Msg) json() map[string]any {
	o := map[string]any{"from": m.From.Hex(), "to": m.To.Hex(), "data": "0x" + hex.EncodeToString(m.Data)}
	if m.Value != nil {
		o["value"] = "0x" + m.Value.Text(16)
	}
	return o
}

// Call is eth_call at latest: the return data, or the node's error (a
// revert is an *RPCError).
func (c *Client) Call(ctx context.Context, m Msg) ([]byte, error) {
	return c.callAt(ctx, m, "latest")
}

// CallAt is eth_call at block n.
func (c *Client) CallAt(ctx context.Context, m Msg, n uint64) ([]byte, error) {
	return c.callAt(ctx, m, fmt.Sprintf("0x%x", n))
}

func (c *Client) callAt(ctx context.Context, m Msg, tag string) ([]byte, error) {
	var s string
	if err := c.call(ctx, "eth_call", &s, m.json(), tag); err != nil {
		return nil, err
	}
	return hexBytes(s)
}

// EstimateGas is eth_estimateGas (a revert is an *RPCError).
func (c *Client) EstimateGas(ctx context.Context, m Msg) (uint64, error) {
	n, err := quantity(ctx, c, "eth_estimateGas", m.json())
	if err != nil {
		return 0, err
	}
	return n.Uint64(), nil
}

// SendRaw is eth_sendRawTransaction; it returns the node's transaction hash.
func (c *Client) SendRaw(ctx context.Context, raw []byte) ([]byte, error) {
	var s string
	if err := c.call(ctx, "eth_sendRawTransaction", &s, "0x"+hex.EncodeToString(raw)); err != nil {
		return nil, err
	}
	return hexBytes(s)
}

// Log is one event log.
type Log struct {
	Address     Address
	Topics      [][32]byte
	Data        []byte
	BlockNumber uint64
	BlockHash   [32]byte
	TxHash      [32]byte
	LogIndex    uint64
	Removed     bool
}

// Receipt is the part of a receipt P2 reads; nil when not mined yet.
type Receipt struct {
	Status          uint64 // 1 success, 0 reverted
	BlockNumber     uint64
	GasUsed         uint64
	ContractAddress *Address // a contract creation's
	Logs            []Log
}

type rawLog struct {
	Address, Data, BlockNumber, BlockHash, TransactionHash, LogIndex string
	Topics                                                           []string
	Removed                                                          bool
}

func (r rawLog) decode() (Log, error) {
	var l Log
	a, err := ParseAddress(r.Address)
	if err != nil {
		return l, err
	}
	l.Address, l.Removed = a, r.Removed
	if l.Data, err = hexBytes(r.Data); err != nil {
		return l, err
	}
	bn, err1 := hexUint(r.BlockNumber)
	li, err2 := hexUint(r.LogIndex)
	th, err3 := hexBytes(r.TransactionHash)
	bh, err4 := hexBytes(r.BlockHash)
	if err := errors.Join(err1, err2, err3, err4); err != nil || len(th) != 32 || len(bh) != 32 {
		return l, fmt.Errorf("log: %v", err)
	}
	l.BlockNumber, l.LogIndex = bn.Uint64(), li.Uint64()
	copy(l.TxHash[:], th)
	copy(l.BlockHash[:], bh)
	for _, t := range r.Topics {
		b, err := hexBytes(t)
		if err != nil || len(b) != 32 {
			return l, fmt.Errorf("topic %q", t)
		}
		var w [32]byte
		copy(w[:], b)
		l.Topics = append(l.Topics, w)
	}
	return l, nil
}

// Receipt is eth_getTransactionReceipt: nil, nil while not mined.
func (c *Client) Receipt(ctx context.Context, hash []byte) (*Receipt, error) {
	var r *struct {
		Status, BlockNumber, GasUsed string
		ContractAddress              *string
		Logs                         []rawLog
	}
	if err := c.call(ctx, "eth_getTransactionReceipt", &r, "0x"+hex.EncodeToString(hash)); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	st, err1 := hexUint(r.Status)
	bn, err2 := hexUint(r.BlockNumber)
	gu, err3 := hexUint(r.GasUsed)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("receipt: %w", err)
	}
	out := &Receipt{Status: st.Uint64(), BlockNumber: bn.Uint64(), GasUsed: gu.Uint64()}
	if r.ContractAddress != nil {
		a, err := ParseAddress(*r.ContractAddress)
		if err != nil {
			return nil, fmt.Errorf("receipt: %w", err)
		}
		out.ContractAddress = &a
	}
	for _, rl := range r.Logs {
		l, err := rl.decode()
		if err != nil {
			return nil, err
		}
		out.Logs = append(out.Logs, l)
	}
	return out, nil
}

// Logs is eth_getLogs for one address and topic 0, between two blocks.
func (c *Client) Logs(ctx context.Context, address Address, topic0 [32]byte, from, to uint64) ([]Log, error) {
	return c.FilterLogs(ctx, address, [][32]byte{topic0}, from, to)
}

// FilterLogs is eth_getLogs for one address whose first topics are topics
// (a zero topic matches any), between two blocks.
func (c *Client) FilterLogs(ctx context.Context, address Address, topics [][32]byte, from, to uint64) ([]Log, error) {
	ts := make([]any, len(topics))
	for i, t := range topics {
		if t != ([32]byte{}) {
			ts[i] = "0x" + hex.EncodeToString(t[:])
		}
	}
	var rs []rawLog
	filter := map[string]any{"address": address.Hex(), "topics": ts,
		"fromBlock": fmt.Sprintf("0x%x", from), "toBlock": fmt.Sprintf("0x%x", to)}
	if err := c.call(ctx, "eth_getLogs", &rs, filter); err != nil {
		return nil, err
	}
	out := make([]Log, 0, len(rs))
	for _, r := range rs {
		l, err := r.decode()
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}
