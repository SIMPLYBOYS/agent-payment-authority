package contracts

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Drift check, not a replacement for an external JSON Schema validator.
func TestSchemaTracksWireFields(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/m1.schema.json")
	mustPass(t, err)
	var schema map[string]any
	mustPass(t, json.Unmarshal(raw, &schema))
	defs := schema["$defs"].(map[string]any)
	for _, value := range []any{Consumer{}, WalletRef{}, Task{}, RegistryConfig{}, AuthorizationIntent{}, Quote{}, PurchaseOrder{}, PolicyDecision{}, EventScope{}, EventPayload{}, AuditEvent{}, ExpectedOutcome{}, AcceptanceCase{}, CaseManifest{}, CommandResult{}, EvidenceRef{}, ClaimResult{}, RunManifest{}} {
		typ := reflect.TypeOf(value)
		t.Run(typ.Name(), func(t *testing.T) {
			def := defs[typ.Name()].(map[string]any)
			properties := def["properties"].(map[string]any)
			required := map[string]bool{}
			if arr, ok := def["required"].([]any); ok {
				for _, key := range arr {
					required[key.(string)] = true
				}
			}
			if len(properties) != typ.NumField() {
				t.Fatal("schema/Go field count drift")
			}
			for n := 0; n < typ.NumField(); n++ {
				field := typ.Field(n)
				tag := strings.Split(field.Tag.Get("json"), ",")
				key := tag[0]
				if _, ok := properties[key]; !ok {
					t.Fatalf("missing schema property %s", key)
				}
				optional := len(tag) > 1 && tag[1] == "omitempty"
				if required[key] == optional {
					t.Fatalf("required-field drift: %s", key)
				}
			}
		})
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for k, x := range v {
				if k == "$ref" {
					name := strings.TrimPrefix(x.(string), "#/$defs/")
					if _, ok := defs[name]; !ok {
						t.Fatalf("unresolved ref %s", x)
					}
				}
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(schema)
}

func TestDecodeRequiredAndCaseSensitiveFields(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"consumer_id":"a","agent_id":"b"}`, `{"consumer_id":"a","agent_id":"b","wallet_id":null}`, `{"consumer_id":"a","agent_id":"b","wallet_id":"c","WALLET_ID":"d"}`} {
		var c Consumer
		mustFail(t, Decode([]byte(raw), &c))
	}
	e, _, _ := event(t)
	raw, err := json.Marshal(e)
	mustPass(t, err)
	var fields map[string]json.RawMessage
	mustPass(t, json.Unmarshal(raw, &fields))
	delete(fields, "payload")
	raw, err = json.Marshal(fields)
	mustPass(t, err)
	var parsed AuditEvent
	mustFail(t, Decode(raw, &parsed))
}
