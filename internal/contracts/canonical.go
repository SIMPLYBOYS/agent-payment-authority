// Package contracts defines M1 fixture contracts. It does not authorize payment.
package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Version = "apa.m1.v1"
const FixtureDomain = "agent-payment-authority/m1-fixture/v1/verifier-not-deployed"
const MaxJSONBytes = 65536
const MaxSafeInteger = 9007199254740991

// Canonical uses the documented APA JSON subset, NOT RFC 8785 or EIP-712.
// Unknown members are retained. Ambiguous/unsupported input is rejected.
func Canonical(raw []byte) ([]byte, error) {
	if len(raw) > MaxJSONBytes || !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid UTF-8 or JSON exceeds %d bytes", MaxJSONBytes)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := readValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	return json.Marshal(v)
}

func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("JSON depth exceeded")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			m := map[string]any{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return nil, err
				}
				key, ok := k.(string)
				if !ok || strings.ContainsRune(key, utf8.RuneError) {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, exists := m[key]; exists {
					return nil, fmt.Errorf("duplicate JSON member %q", key)
				}
				value, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[key] = value
			}
			if end, err := d.Token(); err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("invalid object")
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, err := readValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if end, err := d.Token(); err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("invalid array")
			}
			return a, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter")
		}
	case json.Number:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil || n > MaxSafeInteger || n < -MaxSafeInteger || strconv.FormatInt(n, 10) != string(v) {
			return nil, fmt.Errorf("JSON numbers must be canonical safe integers; use strings for amounts")
		}
		return v, nil
	case string:
		// encoding/json replaces malformed surrogate escapes with U+FFFD. Reject
		// that scalar (including literal U+FFFD) instead of hashing repaired text.
		if strings.ContainsRune(v, utf8.RuneError) {
			return nil, fmt.Errorf("replacement character / invalid surrogate not supported")
		}
		return v, nil
	case nil, bool:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value")
	}
}

// Decode rejects duplicate keys, unknown typed fields and trailing documents.
// Semantic validation still needs the appropriate Validate method / registry.
func Decode(raw []byte, dst any) error {
	canonical, err := Canonical(raw)
	if err != nil {
		return err
	}
	if err := requiredFields(canonical, reflect.TypeOf(dst)); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

// encoding/json alone treats absent/null fields as zero values. Enforce the
// required/non-null wire shape recursively; omitempty fields may be absent.
func requiredFields(raw []byte, typ reflect.Type) error {
	if typ == nil {
		return fmt.Errorf("nil destination")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if bytes.Equal(raw, []byte("null")) {
		return fmt.Errorf("null typed field")
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return err
		}
		allowed := map[string]bool{}
		for n := 0; n < typ.NumField(); n++ {
			field := typ.Field(n)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			allowed[name] = true
			value, ok := fields[name]
			if !ok {
				if len(tag) > 1 && tag[1] == "omitempty" {
					continue
				}
				return fmt.Errorf("missing required field %s", name)
			}
			if err := requiredFields(value, field.Type); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		for name := range fields {
			if !allowed[name] {
				return fmt.Errorf("unknown field %s", name)
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := requiredFields(v, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, v := range values {
			if err := requiredFields(v, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func Digest(domain string, raw []byte) (string, error) {
	if domain == "" || strings.ContainsRune(domain, 0) {
		return "", fmt.Errorf("invalid digest domain")
	}
	c, err := Canonical(raw)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(c)
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func digestValue(domain string, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return Digest(domain, raw)
}
