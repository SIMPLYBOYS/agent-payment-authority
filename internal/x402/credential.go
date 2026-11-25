package x402

// A shop's credential requirement in its 402 (x402 v2 boundary 4,
// docs/commercial-credential-ext.md): the attributes a venue asks the buyer's
// credential to disclose, under extensions["apa-credential"].info. No x402
// extension for this is published, so the key is this project's own; a
// client that does not know it ignores it, as extensions allow.

import (
	"encoding/json"
	"errors"
)

// CredentialKey is the extension's key in extensions.
const CredentialKey = "apa-credential"

// CredentialRequirement: the credential schema and, by attribute, the value
// it must have ("" any value, disclosed only).
type CredentialRequirement struct {
	Schema   string            `json:"schema"`
	Requires map[string]string `json:"requires"`
}

type credentialExt struct {
	Info CredentialRequirement `json:"info"`
}

// CredentialMember is the extension's member for extensions.
func CredentialMember(r CredentialRequirement) (json.RawMessage, error) {
	return json.Marshal(credentialExt{Info: r})
}

// CredentialIn reads the requirement out of extensions: nil (no error)
// when the shop declares none; an error when it is malformed.
func CredentialIn(ext json.RawMessage) (*CredentialRequirement, error) {
	var all map[string]json.RawMessage
	if len(ext) == 0 || string(ext) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(ext, &all); err != nil {
		return nil, errors.New("extensions is not an object")
	}
	raw, ok := all[CredentialKey]
	if !ok {
		return nil, nil
	}
	var e credentialExt
	if err := json.Unmarshal(raw, &e); err != nil || e.Info.Schema == "" || len(e.Info.Requires) == 0 {
		return nil, errors.New("the apa-credential extension is malformed")
	}
	return &e.Info, nil
}

// WithinCap: what a shop asks stays inside what the host allows its venue
// (allowed: attribute -> value, "" any): only attributes the host lists,
// and a value the host fixed is the shop's too. The host never discloses
// more than it allows because a shop asked.
func WithinCap(asked, allowed map[string]string) error {
	for k, v := range asked {
		a, ok := allowed[k]
		switch {
		case !ok:
			return errors.New("the shop asks for an attribute the host does not allow it: " + k)
		case a != "" && v != a:
			return errors.New("the shop asks for another value than the host's: " + k)
		}
	}
	return nil
}

// The buyer's answer, in its PaymentPayload: extensions["apa-credential"].info.presentation,
// a presentation the shop verifies itself (credential.VerifyAtShop), opaque here.
type presentationExt struct {
	Info struct {
		Presentation json.RawMessage `json:"presentation"`
	} `json:"info"`
}

// Echo is a PaymentPayload's extensions for a 402's (v2 5.1.2: a client
// includes at least the info it received, adds to it, deletes or overwrites
// nothing): the 402's as they are, and, when p is not nil, the
// presentation added to apa-credential's info beside what the shop asked.
func Echo(required, p json.RawMessage) (json.RawMessage, error) {
	all := map[string]json.RawMessage{}
	if len(required) > 0 && string(required) != "null" {
		if err := json.Unmarshal(required, &all); err != nil {
			return nil, errors.New("extensions is not an object")
		}
	}
	if p != nil {
		member := map[string]json.RawMessage{}
		if raw, ok := all[CredentialKey]; ok {
			if err := json.Unmarshal(raw, &member); err != nil {
				return nil, errors.New("the apa-credential extension is malformed")
			}
		}
		info := map[string]json.RawMessage{}
		if raw, ok := member["info"]; ok {
			if err := json.Unmarshal(raw, &info); err != nil {
				return nil, errors.New("the apa-credential extension is malformed")
			}
		}
		if _, ok := info["presentation"]; ok {
			return nil, errors.New("the 402 already carries a presentation")
		}
		info["presentation"] = p
		var err error
		if member["info"], err = json.Marshal(info); err != nil {
			return nil, err
		}
		if all[CredentialKey], err = json.Marshal(member); err != nil {
			return nil, err
		}
	}
	return json.Marshal(all)
}

// PresentationIn reads the presentation out of a PaymentPayload's
// extensions: nil (no error) when there is none.
func PresentationIn(ext json.RawMessage) (json.RawMessage, error) {
	var all map[string]json.RawMessage
	if len(ext) == 0 || string(ext) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(ext, &all); err != nil {
		return nil, errors.New("extensions is not an object")
	}
	raw, ok := all[CredentialKey]
	if !ok {
		return nil, nil
	}
	var e presentationExt
	if err := json.Unmarshal(raw, &e); err != nil || len(e.Info.Presentation) == 0 {
		return nil, errors.New("the apa-credential extension carries no presentation")
	}
	return e.Info.Presentation, nil
}
