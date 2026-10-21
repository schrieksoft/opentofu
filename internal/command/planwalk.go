// Copyright (c) The OpenTofu Authors
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2023 HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/opentofu/opentofu/internal/backend"
	"github.com/opentofu/opentofu/internal/command/arguments"
	"github.com/opentofu/opentofu/internal/command/views"
	"github.com/opentofu/opentofu/internal/configs/configload"
	"github.com/opentofu/opentofu/internal/encryption"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/planwalk"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
)

func PlanWalkCommander() Command {
	cmd := Command{
		Name:  "planwalk",
		Short: "Show what an upstream root's predicted changes would do here",
		Long: `Plans the configuration against the values another root's plan predicts for this root's variables, and reports what that does to this root's own outputs.

The predicted values are carried in as unknowns, which a variables file cannot express, so a value that will change is planned as changing rather than as some stand-in. Nothing is written to state and no plan file is produced.`,

		GroupID: OtherCommandGroup.ID,
	}

	args := arguments.BindPlanWalk(&cmd.CommandLine)
	cmd.Run = func(meta Meta) int {
		return PlanWalkCommand{meta}.Execute(args, views.NewPlan(args.View, meta.View))
	}

	return cmd
}

// PlanWalkCommand plans a root with an upstream's predicted values supplied as
// unknowns, and writes the output changes a downstream root would consume.
type PlanWalkCommand struct {
	Meta
}

func (c PlanWalkCommand) Execute(args *arguments.PlanWalk, view views.Plan) int {
	var diags tfdiags.Diagnostics
	ctx := c.CommandContext()

	var err error
	if c.pluginPath, err = c.loadPluginPath(); err != nil {
		diags = diags.Append(err)
		view.Diagnostics(diags)
		return 1
	}

	diags = diags.Append(c.providerDevOverrideRuntimeWarnings())

	predicted, moreDiags := loadPredictedValues(args.SignalsIn)
	diags = diags.Append(moreDiags)
	if diags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	enc, encDiags := c.Encryption(ctx)
	diags = diags.Append(encDiags)
	if encDiags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	be, beDiags := c.PrepareBackend(ctx, args.State, view, enc)
	diags = diags.Append(beDiags)
	if diags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	opReq, opDiags := c.OperationRequest(ctx, be, view, args.View, args.Operation, enc, args.Quiet || args.SignalsOut == "-")
	diags = diags.Append(opDiags)
	if diags.HasErrors() {
		view.Diagnostics(diags)
		return 1
	}

	view.Diagnostics(diags)
	diags = nil

	// Above every collected source, because they are the change being tested.
	c.overrideVariableValues = predicted

	if args.SignalsOut != "" {
		opReq.InspectPlan = func(plan *plans.Plan) error {
			changes, err := planwalk.OutputChangesFor(plan)
			if err != nil {
				return err
			}
			return planwalk.WriteOutputChanges(args.SignalsOut, changes)
		}
	}

	op, diags := c.RunOperation(ctx, be, opReq)
	view.Diagnostics(diags)
	if diags.HasErrors() {
		return 1
	}

	if op.Result != backend.OperationSuccess {
		return op.Result.ExitStatus()
	}

	return op.Result.ExitStatus()
}

func (c *PlanWalkCommand) PrepareBackend(ctx context.Context, args *arguments.State, view views.Plan, enc encryption.Encryption) (backend.Enhanced, tfdiags.Diagnostics) {
	backendConfig, diags := c.loadBackendConfig(ctx, ".")
	if diags.HasErrors() {
		return nil, diags
	}

	be, beDiags := c.Backend(ctx, &BackendOpts{
		Config: backendConfig,
		View:   view.Backend(),
	}, enc.State())
	diags = diags.Append(beDiags)
	if beDiags.HasErrors() {
		return nil, diags
	}

	return be, diags
}

// OperationRequest builds the plan operation. PlanRefresh is false, which the
// local backend reads as SkipRefresh: a probe asks what a change would do, so
// it never reads live resources and never writes state.
func (c *PlanWalkCommand) OperationRequest(
	ctx context.Context,
	be backend.Enhanced,
	view views.Plan,
	viewOptions *arguments.View,
	args *arguments.Operation,
	enc encryption.Encryption,
	quiet bool,
) (*backend.Operation, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	opReq := c.Operation(ctx, be, view.Backend(), enc)
	opReq.ConfigDir = "."
	opReq.PlanMode = args.PlanMode
	opReq.Hooks = view.Hooks()
	opReq.PlanRefresh = false
	opReq.Targets = args.Targets
	opReq.Excludes = args.Excludes
	opReq.Type = backend.OperationTypePlan
	opReq.View = view.Operation()
	if quiet {
		// Signals on stdout cannot share it with the rendered plan, and a caller
		// wanting only the signals has no use for it either.
		opReq.View = quietPlanOperation{opReq.View}
	}

	var err error
	opReq.ConfigLoader, err = configload.Initialise(c.configLoader())
	if err != nil {
		diags = diags.Append(fmt.Errorf("Failed to initialize config loader: %w", err))
		return nil, diags
	}

	return opReq, diags
}

// loadPredictedValues reads a signals document and presents each variable it
// names as a variable value the plan can consume.
func loadPredictedValues(path string) (map[string]backend.UnparsedVariableValue, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	if path == "" {
		return nil, diags
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		diags = diags.Append(fmt.Errorf("read signals file: %w", err))
		return nil, diags
	}

	var in planwalk.Inputs
	if err := json.Unmarshal(raw, &in); err != nil {
		diags = diags.Append(fmt.Errorf("parse signals file %s: %w", path, err))
		return nil, diags
	}

	if in.FormatVersion != planwalk.FormatVersion {
		diags = diags.Append(fmt.Errorf(
			"signals file %s has format_version %q, which this version does not know how to read",
			path, in.FormatVersion))
		return nil, diags
	}

	values := map[string]backend.UnparsedVariableValue{}
	for name, value := range in.Variables {
		values[name] = planwalk.PredictedValue{Name: name, Value: value}
	}

	return values, diags
}

// quietPlanOperation passes everything through but drops the rendered plan.
type quietPlanOperation struct {
	views.Operation
}

func (q quietPlanOperation) Plan(*plans.Plan, *tofu.Schemas) {}

func (q quietPlanOperation) PlanNextStep(string, string) {}
