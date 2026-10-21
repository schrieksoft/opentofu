// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package arguments

// PlanWalk represents the command-line arguments for the planwalk command.
type PlanWalk struct {
	// State, Operation and Vars are the common extended flags, so variables
	// resolve exactly as they do for a plan.
	State     *State
	Operation *Operation
	Vars      *Vars

	// SignalsIn names a file holding the values upstream roots predict for this
	// root's variables. They are overlaid on the normally resolved variables,
	// above every other source, because they are the change being tested.
	SignalsIn string

	// SignalsOut names the file the resulting output changes are written to.
	// "-" sends them to stdout and moves the rendered plan to stderr, so stdout
	// stays pipeable.
	SignalsOut string

	// Quiet suppresses the rendered plan, so only the signals document is
	// produced. Needed when signals go to stdout: a caller piping them cannot
	// also receive the plan there.
	Quiet bool

	// View represents the global view options
	View *View
}

// BindPlanWalk registers CLI arguments, returning a PlanWalk value.
func BindPlanWalk(cli *CommandLine) *PlanWalk {
	pw := PlanWalk{
		View:      BindView(cli, viewFlagAll),
		Operation: BindOperation(cli),
		Vars:      BindVars(cli),
		State:     BindState(cli, stateFlagAll),
	}

	cli.StringVar(&pw.SignalsIn, "signals-in", "",
		`Load predicted variable values from the given file. These take precedence over every other source.`,
	).SetDisplay("=filename")

	cli.StringVar(&pw.SignalsOut, "signals-out", "",
		`Write this root's own predicted output changes to the given file, or to stdout with "-".`,
	).SetDisplay("=filename")

	cli.BoolVar(&pw.Quiet, "quiet", false,
		`Do not render the plan, leaving only the signals document.`,
	)

	return &pw
}
