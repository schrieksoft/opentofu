// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package signals

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// unknownMarker is the member that tells a known value from one a plan cannot
// predict. A known value is its own JSON; JSON has no scalar for an unknown, so
// one is written as an object carrying this.
const unknownMarker = "$unknown"

// Refinement is what a plan knows about a value it cannot yet state: that it
// will not be null, that it starts with a given prefix, or that it falls within
// bounds. cty offers exactly these, and each is plain data.
type Refinement struct {
	NotNull bool `json:"not_null,omitempty"`
	Null    bool `json:"null,omitempty"`

	Prefix string `json:"prefix,omitempty"`

	NumberMin          *json.Number `json:"number_min,omitempty"`
	NumberMinInclusive bool         `json:"number_min_inclusive,omitempty"`
	NumberMax          *json.Number `json:"number_max,omitempty"`
	NumberMaxInclusive bool         `json:"number_max_inclusive,omitempty"`

	LengthMin *int `json:"length_min,omitempty"`
	LengthMax *int `json:"length_max,omitempty"`
}

// readableValue renders a value as JSON a person can read: the value itself
// when the plan knows it, and a marked object when it does not.
func readableValue(val cty.Value, ty cty.Type) (json.RawMessage, error) {
	if val.IsKnown() {
		// Marshal handles every known value, structured ones included, and is
		// the only encoder involved for them.
		b, err := ctyjson.Marshal(val, ty)
		if err != nil {
			return nil, err
		}
		return b, nil
	}

	marked := map[string]any{unknownMarker: true}
	if r := refinementOf(val); r != nil {
		marked["refinement"] = r
	}

	b, err := json.Marshal(marked)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// refinementOf reads whatever a plan has narrowed an unknown down to, or nil
// when it has narrowed nothing.
func refinementOf(val cty.Value) *Refinement {
	rng := val.Range()
	ty := val.Type()

	var r Refinement
	var any bool

	if rng.DefinitelyNotNull() {
		r.NotNull = true
		any = true
	}

	if ty == cty.String {
		if prefix := rng.StringPrefix(); prefix != "" {
			r.Prefix = prefix
			any = true
		}
	}

	if ty == cty.Number {
		if min, inclusive := rng.NumberLowerBound(); min.IsKnown() && !min.IsNull() {
			n := json.Number(min.AsBigFloat().Text('f', -1))
			r.NumberMin = &n
			r.NumberMinInclusive = inclusive
			any = true
		}
		if max, inclusive := rng.NumberUpperBound(); max.IsKnown() && !max.IsNull() {
			n := json.Number(max.AsBigFloat().Text('f', -1))
			r.NumberMax = &n
			r.NumberMaxInclusive = inclusive
			any = true
		}
	}

	if ty.IsCollectionType() {
		if min := rng.LengthLowerBound(); min > 0 {
			r.LengthMin = &min
			any = true
		}
		if max := rng.LengthUpperBound(); max < cty.UnknownVal(ty).Range().LengthUpperBound() {
			r.LengthMax = &max
			any = true
		}
	}

	if !any {
		return nil
	}
	return &r
}

// fromReadable turns a readable value back into a cty value, rebuilding an
// unknown and its refinements from the marked form.
func fromReadable(raw json.RawMessage, ty cty.Type) (cty.Value, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err == nil {
		if _, marked := probe[unknownMarker]; marked {
			return unknownFromReadable(probe, ty)
		}
	}

	return ctyjson.Unmarshal(raw, ty)
}

func unknownFromReadable(probe map[string]json.RawMessage, ty cty.Type) (cty.Value, error) {
	val := cty.UnknownVal(ty)

	rawRefinement, ok := probe["refinement"]
	if !ok {
		return val, nil
	}

	var r Refinement
	if err := json.Unmarshal(rawRefinement, &r); err != nil {
		return cty.NilVal, fmt.Errorf("parse refinement: %w", err)
	}

	b := val.Refine()

	switch {
	case r.Null:
		b = b.Null()
	case r.NotNull:
		b = b.NotNull()
	}

	if r.Prefix != "" && ty == cty.String {
		b = b.StringPrefix(r.Prefix)
	}

	if ty == cty.Number {
		if r.NumberMin != nil {
			if f, _, err := big.ParseFloat(r.NumberMin.String(), 10, 512, big.ToNearestEven); err == nil {
				b = b.NumberRangeLowerBound(cty.NumberVal(f), r.NumberMinInclusive)
			}
		}
		if r.NumberMax != nil {
			if f, _, err := big.ParseFloat(r.NumberMax.String(), 10, 512, big.ToNearestEven); err == nil {
				b = b.NumberRangeUpperBound(cty.NumberVal(f), r.NumberMaxInclusive)
			}
		}
	}

	if ty.IsCollectionType() {
		if r.LengthMin != nil {
			b = b.CollectionLengthLowerBound(*r.LengthMin)
		}
		if r.LengthMax != nil {
			b = b.CollectionLengthUpperBound(*r.LengthMax)
		}
	}

	return b.NewValue(), nil
}
