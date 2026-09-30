package contracts

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
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
	for _, value := range []any{Consumer{}, WalletRef{}, Asset{}, Task{}, RegistryConfig{}, AuthorizationIntent{}, Quote{}, PurchaseOrder{}, PolicyDecision{}, EventScope{}, EventPayload{}, AuditEvent{}, ExpectedOutcome{}, AcceptanceCase{}, CaseManifest{}, CommandResult{}, EvidenceRef{}, ClaimResult{}, RunManifest{}} {
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

// Closed vocabularies are defined once in Go; the schema must list the same
// values (C9: the field-name check above does not compare enums).
func TestSchemaEnumsTrackGo(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/m1.schema.json")
	mustPass(t, err)
	var schema map[string]any
	mustPass(t, json.Unmarshal(raw, &schema))
	at := func(path ...string) any {
		var v any = schema["$defs"]
		for _, k := range path {
			v = v.(map[string]any)[k]
		}
		return v
	}
	strs := func(v any) []string {
		var out []string
		for _, x := range v.([]any) {
			out = append(out, x.(string))
		}
		return slices.Sorted(slices.Values(out))
	}
	sorted := func(v []string) []string { return slices.Sorted(slices.Values(v)) }
	for name, pair := range map[string][2][]string{
		"task status":      {strs(at("Task", "properties", "status", "enum")), sorted(taskStatuses)},
		"event type":       {strs(at("AuditEvent", "properties", "event_type", "enum")), sorted(eventTypes)},
		"evidence mode":    {strs(at("Mode", "enum")), sorted(evidenceModes)},
		"approval status":  {strs(at("ApprovalStatus", "enum")), sorted(approvalStatuses)},
		"policy verdict":   {strs(at("PolicyDecision", "properties", "policy_verdict", "enum")), sorted(policyVerdicts)},
		"operation":        {strs(at("AcceptanceCase", "properties", "operation", "enum")), keysOf(operations)},
		"defaults":         {strs(at("CaseManifest", "properties", "defaults", "required")), keysOf(policyParams)},
		"defaults entries": {keysOf(at("CaseManifest", "properties", "defaults", "properties").(map[string]any)), keysOf(policyParams)},
	} {
		if !slices.Equal(pair[0], pair[1]) {
			t.Fatalf("%s drift: schema %v, Go %v", name, pair[0], pair[1])
		}
	}
	if at("Version", "const") != Version || at("AuthorizationIntent", "properties", "domain", "const") != FixtureDomain {
		t.Fatal("schema version or fixture domain drift")
	}
}
