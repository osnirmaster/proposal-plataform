package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"proposalplatform/internal/database"
	"proposalplatform/internal/mapping"
	"proposalplatform/internal/recipe"
)

// Input para consolidar o resultado de um step.
type Input struct {
	TenantID       string                 `json:"tenantId"`
	ProposalID     string                 `json:"proposalId"`
	JourneyType    string                 `json:"journeyType"`
	JourneyVersion string                 `json:"journeyVersion"`
	Step           map[string]interface{} `json:"step"`
	Outcome        string                 `json:"outcome"`
	Result         map[string]interface{} `json:"result"`
}

// Output informa se o fluxo é terminal.
type Output struct {
	IsTerminal     bool     `json:"isTerminal"`
	TerminalStatus string   `json:"terminalStatus,omitempty"`
	ReasonCodes    []string `json:"reasonCodes"`
}

func handler(ctx context.Context, in Input) (Output, error) {
	db, err := database.New(ctx)
	if err != nil {
		return Output{}, err
	}
	nameAny := in.Step["name"]
	name, _ := nameAny.(string)
	// Atualiza o step
	step := &database.Step{
		ProposalID:  in.ProposalID,
		Name:        name,
		Attempt:     1,
		State:       "COMPLETED",
		Outcome:     in.Outcome,
		Result:      in.Result,
		StartedAt:   "", // desconhecido nesta chamada
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := db.PutStep(ctx, step); err != nil {
		return Output{}, err
	}

	prop, err := db.GetProposal(ctx, in.TenantID, in.ProposalID)
	if err != nil {
		return Output{}, err
	}
	if patch, ok := in.Result["contextPatch"].(map[string]interface{}); ok && len(patch) > 0 {
		if prop.Context == nil {
			prop.Context = map[string]interface{}{}
		}
		if err := mapping.MergeAtPath(prop.Context, "$.context", patch); err != nil {
			return Output{}, err
		}
		if err := db.PutProposal(ctx, prop); err != nil {
			return Output{}, err
		}
	}

	steps, err := db.GetSteps(ctx, in.ProposalID)
	if err != nil {
		return Output{}, err
	}
	completed := make([]recipe.CompletedStep, 0, len(steps))
	for _, s := range steps {
		completed = append(completed, recipe.CompletedStep{
			Name:    s.Name,
			State:   s.State,
			Outcome: s.Outcome,
		})
	}
	journeyRecipe, err := recipe.Load(in.JourneyType, in.JourneyVersion)
	if err != nil {
		return Output{}, err
	}
	decision, err := journeyRecipe.Resolve(completed, prop.Context)
	if err != nil {
		return Output{}, err
	}

	if decision.Type != "TERMINAL" {
		if decision.ProposalStatus != "" {
			if !journeyRecipe.CanTransition(prop.Status, decision.ProposalStatus) {
				return Output{}, fmt.Errorf("invalid proposal status transition: %s -> %s", prop.Status, decision.ProposalStatus)
			}
			prop.Status = decision.ProposalStatus
			if err := db.PutProposal(ctx, prop); err != nil {
				return Output{}, err
			}
		}
		return Output{IsTerminal: false, ReasonCodes: []string{}}, nil
	}
	reasonCodes := decision.ReasonCodes
	if reasonCodes == nil {
		reasonCodes = []string{}
	}

	targetStatus := decision.TerminalStatus
	if decision.ProposalStatus != "" {
		targetStatus = decision.ProposalStatus
	}
	if !journeyRecipe.CanTransition(prop.Status, targetStatus) {
		return Output{}, fmt.Errorf("invalid proposal status transition: %s -> %s", prop.Status, targetStatus)
	}
	prop.Status = targetStatus
	if err := db.PutProposal(ctx, prop); err != nil {
		return Output{}, err
	}
	return Output{
		IsTerminal:     true,
		TerminalStatus: decision.TerminalStatus,
		ReasonCodes:    reasonCodes,
	}, nil
}

func main() {
	lambda.Start(handler)
}
