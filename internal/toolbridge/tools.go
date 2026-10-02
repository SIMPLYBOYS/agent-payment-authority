package toolbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// Tool is an MCP tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

const productArg = `{"type":"object","properties":{"product_id":{"type":"string","pattern":"^[a-z0-9-]{1,64}$"}},"required":["product_id"],"additionalProperties":false}`

// Tools is the whole surface the model gets: no HTTP, shell, file, SQL,
// event or host-management tool exists.
var Tools = []Tool{
	{"list_products", "List the products of the trusted shop (titles and display prices; no paid content).",
		json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
	{"get_quote", "Fetch the shop's current HTTP 402 quote for one listed product. Nothing is paid.", json.RawMessage(productArg)},
	{"request_purchase", "Ask the host to record a purchase request for one listed product. The host decides; a human approves; this tool cannot pay or deliver content.", json.RawMessage(productArg)},
}

func known(name string) bool {
	return slices.ContainsFunc(Tools, func(t Tool) bool { return t.Name == name })
}

var productID = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// parseArgs accepts exactly the tool's schema: no other member, no
// duplicate key, nothing that could carry an identity, URL or price.
func parseArgs(name string, raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	c, err := contracts.Canonical(raw)
	if err != nil || len(c) == 0 || c[0] != '{' {
		return "", errors.New("arguments must be a JSON object")
	}
	var args map[string]json.RawMessage
	json.Unmarshal(c, &args)
	if name == "list_products" {
		if len(args) != 0 {
			return "", errors.New("list_products takes no arguments")
		}
		return "", nil
	}
	var id string
	if len(args) != 1 || json.Unmarshal(args["product_id"], &id) != nil || !productID.MatchString(id) {
		return "", errors.New("the only argument is product_id")
	}
	return id, nil
}
