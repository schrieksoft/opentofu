// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package arguments

// Signals represents the command-line arguments for the signals command.
type Signals struct {
	// PlanPath is the saved plan file to read.
	PlanPath string

	// SignalsOut names the file to write the signals document to, or "-" for
	// stdout. Named as "plan -signals-out" is, since it writes the same thing.
	SignalsOut string

	// View represents the global view options
	View *View
}

// BindSignals registers CLI arguments, returning a Signals value.
func BindSignals(cli *CommandLine) *Signals {
	s := Signals{
		View: BindView(cli, viewFlagNone),
	}

	cli.StringVar(&s.PlanPath, "plan", "",
		`The saved plan file to read, as written by "plan -out".`,
	).SetDisplay("=path")

	cli.StringVar(&s.SignalsOut, "signals-out", "-",
		`Write the signals document to the given path, or to stdout with "-" (the default).`,
	).SetDisplay("=path")

	return &s
}
