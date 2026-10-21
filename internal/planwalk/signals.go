// Package signals is the wire format passed between roots.
//
// A value may be unknown, which is the whole point: an unknown carries the fact
// that a value will change without claiming what it will change to. JSON has no
// representation for that, so values are encoded with cty's msgpack codec and
// base64'd. Refinements (nullness, string prefix, number and length bounds)
// survive the round trip; a string prefix longer than 256 bytes is truncated by
// the codec.
//
// Callers that route these between roots are expected to move Value verbatim
// without decoding it.
package planwalk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	ctymsgpack "github.com/zclconf/go-cty/cty/msgpack"
)

// Action is what a plan says will happen to a value.
type Action string

const (
	NoOp    Action = "no-op"
	Create  Action = "create"
	Update  Action = "update"
	Delete  Action = "delete"
	Unknown Action = "unknown"
)

// Value is one output change or one variable value.
//
// Action is derived from Before and After and is carried for readability; a
// consumer should trust the encoded values over it.
type Value struct {
	Action Action `json:"action"`
	Type   []byte `json:"type"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`

	// Sensitive marks a value the upstream root declared sensitive. It is
	// carried so a consumer can avoid printing it; the encoded value is not
	// itself protected.
	Sensitive bool `json:"sensitive,omitempty"`
}

// OutputChanges is what a root emits: keyed by output name.
type OutputChanges struct {
	FormatVersion string           `json:"format_version"`
	Outputs       map[string]Value `json:"outputs"`
}

// Inputs is what a root consumes: keyed by variable name. The orchestrator
// builds this from one or more OutputChanges by renaming keys according to the
// dependency graph, which only it knows.
type Inputs struct {
	FormatVersion string           `json:"format_version"`
	Variables     map[string]Value `json:"variables"`
}

const FormatVersion = "1"

// Encode builds a Value from a before/after pair. Either may be cty.NilVal to
// mean absent.
func Encode(before, after cty.Value) (Value, error) {
	ty := cty.DynamicPseudoType
	switch {
	case after != cty.NilVal:
		ty = after.Type()
	case before != cty.NilVal:
		ty = before.Type()
	}

	tyJSON, err := ctyjson.MarshalType(ty)
	if err != nil {
		return Value{}, fmt.Errorf("marshal type: %w", err)
	}

	v := Value{Action: action(before, after), Type: tyJSON}

	if before != cty.NilVal {
		if v.Before, err = encodeValue(before, ty); err != nil {
			return Value{}, fmt.Errorf("marshal before: %w", err)
		}
	}
	if after != cty.NilVal {
		if v.After, err = encodeValue(after, ty); err != nil {
			return Value{}, fmt.Errorf("marshal after: %w", err)
		}
	}
	return v, nil
}

// DecodeAfter returns the planned new value, which is what a downstream root
// needs. An absent After decodes as an unknown of the declared type, since a
// value the upstream plan could not predict is exactly an unknown.
func (v Value) DecodeAfter() (cty.Value, error) {
	ty, err := v.decodeType()
	if err != nil {
		return cty.NilVal, err
	}
	if v.After == "" {
		return cty.UnknownVal(ty), nil
	}
	return decodeValue(v.After, ty)
}

// DecodeBefore returns the prior value, or cty.NilVal if there was none.
func (v Value) DecodeBefore() (cty.Value, error) {
	if v.Before == "" {
		return cty.NilVal, nil
	}
	ty, err := v.decodeType()
	if err != nil {
		return cty.NilVal, err
	}
	return decodeValue(v.Before, ty)
}

func (v Value) decodeType() (cty.Type, error) {
	ty, err := ctyjson.UnmarshalType(v.Type)
	if err != nil {
		return cty.NilType, fmt.Errorf("unmarshal type: %w", err)
	}
	return ty, nil
}

func encodeValue(val cty.Value, ty cty.Type) (string, error) {
	b, err := ctymsgpack.Marshal(val, ty)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func decodeValue(s string, ty cty.Type) (cty.Value, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return cty.NilVal, fmt.Errorf("decode base64: %w", err)
	}
	val, err := ctymsgpack.Unmarshal(b, ty)
	if err != nil {
		return cty.NilVal, fmt.Errorf("unmarshal value: %w", err)
	}
	return val, nil
}

func action(before, after cty.Value) Action {
	switch {
	case before == cty.NilVal && after == cty.NilVal:
		return NoOp
	case before == cty.NilVal:
		return Create
	case after == cty.NilVal:
		return Delete
	case !after.IsKnown():
		return Unknown
	case before.IsKnown() && before.RawEquals(after):
		return NoOp
	default:
		return Update
	}
}

// ReadInputs parses an Inputs document.
func ReadInputs(b []byte) (*Inputs, error) {
	var in Inputs
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, fmt.Errorf("parse inputs: %w", err)
	}
	if in.FormatVersion != "" && in.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("unsupported format_version %q, want %q", in.FormatVersion, FormatVersion)
	}
	if in.Variables == nil {
		in.Variables = map[string]Value{}
	}
	return &in, nil
}
