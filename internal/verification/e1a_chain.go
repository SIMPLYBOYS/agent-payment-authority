package verification

// Chain is what E4 and E8 read from the chain (docs/commercial-e1a.md 4.5).
type Chain interface{}

// e1aOnChain is E4 and E8; without a chain both are INCONCLUSIVE.
func e1aOnChain(f *findings, m E1aManifest, runs map[string]*e1aRun, chain Chain) {
	if chain == nil {
		f.missing("E4", nil, "no --rpc: tx#1, tx#2 and the proof were not read from the chain")
		f.missing("E8", nil, "no --rpc: the wallets' transfers were not counted on the chain")
	}
}
