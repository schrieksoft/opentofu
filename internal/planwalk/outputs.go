// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package planwalk

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/opentofu/opentofu/internal/plans"
)

// OutputChangesFor reads a plan's root output changes into the wire format a
// downstream root consumes. Every output is carried, not only the changed ones:
// a consumer needs a value for each variable it reads, and an unchanged output
// is a no-op rather than an absence.
func OutputChangesFor(plan *plans.Plan) (*OutputChanges, error) {
	out := &OutputChanges{
		FormatVersion: FormatVersion,
		Outputs:       map[string]Value{},
	}

	if plan == nil || plan.Changes == nil {
		return out, nil
	}

	for _, src := range plan.Changes.Outputs {
		if !src.Addr.Module.IsRoot() {
			continue
		}
		name := src.Addr.OutputValue.Name

		change, err := src.Decode()
		if err != nil {
			return nil, fmt.Errorf("decode output %s: %w", name, err)
		}

		v, err := Encode(change.Before, change.After)
		if err != nil {
			return nil, fmt.Errorf("encode output %s: %w", name, err)
		}
		v.Sensitive = src.Sensitive

		out.Outputs[name] = v
	}

	return out, nil
}

// WriteOutputChanges writes the signals document, to stdout when path is "-".
func WriteOutputChanges(path string, changes *OutputChanges) error {
	encoded, err := json.MarshalIndent(changes, "", "  ")
	if err != nil {
		return fmt.Errorf("encode signals: %w", err)
	}
	encoded = append(encoded, '\n')

	if path == "-" {
		_, err := os.Stdout.Write(encoded)
		return err
	}

	return os.WriteFile(path, encoded, 0o600)
}
