// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package command

import (
	"fmt"

	"github.com/opentofu/opentofu/internal/command/arguments"
	"github.com/opentofu/opentofu/internal/command/views"
	"github.com/opentofu/opentofu/internal/plans/planfile"
	"github.com/opentofu/opentofu/internal/signals"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

func SignalsCommander() Command {
	cmd := Command{
		Name:  "signals",
		Short: "Read what a saved plan predicts for a root's outputs",
		Long: `Reads a saved plan file and writes what it predicts for each of this root's outputs, in the form another root consumes.

This is what "plan -signals-out" writes, taken from a plan that has already been saved rather than from a plan being made now. A value the plan cannot predict is reported as unknown, which is the part a variables file cannot express.`,

		GroupID: OtherCommandGroup.ID,
	}

	args := arguments.BindSignals(&cmd.CommandLine)
	cmd.Run = func(meta Meta) int {
		return SignalsCommand{meta}.Execute(args, views.NewShow(args.View, meta.View))
	}

	return cmd
}

// SignalsCommand reads a saved plan file and reports the output changes a
// downstream root would consume.
type SignalsCommand struct {
	Meta
}

func (c SignalsCommand) Execute(args *arguments.Signals, view views.Show) int {
	var diags tfdiags.Diagnostics
	ctx := c.CommandContext()

	if args.PlanPath == "" {
		diags = diags.Append(fmt.Errorf("a plan file is required: pass -plan=path"))
		view.Diagnostics(diags)
		return 1
	}

	enc, encDiags := c.Encryption(ctx)
	diags = diags.Append(encDiags)
	if encDiags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	pf, err := planfile.OpenWrapped(args.PlanPath, enc.Plan())
	if err != nil {
		diags = diags.Append(fmt.Errorf("read plan file: %w", err))
		view.Diagnostics(diags)
		return 1
	}

	local, ok := pf.Local()
	if !ok {
		diags = diags.Append(fmt.Errorf(
			"%s is a cloud plan, whose contents are not available locally", args.PlanPath))
		view.Diagnostics(diags)
		return 1
	}

	// Only the output changes, so a plan written by another version can be read:
	// the whole-plan reader requires an exact version match.
	outputChanges, err := local.ReadOutputChanges()
	if err != nil {
		diags = diags.Append(fmt.Errorf("read plan: %w", err))
		view.Diagnostics(diags)
		return 1
	}

	changes, err := signals.OutputChangesFrom(outputChanges)
	if err != nil {
		diags = diags.Append(err)
		view.Diagnostics(diags)
		return 1
	}

	if err := signals.WriteOutputChanges(args.SignalsOut, changes); err != nil {
		diags = diags.Append(err)
		view.Diagnostics(diags)
		return 1
	}

	view.Diagnostics(diags)
	return 0
}
