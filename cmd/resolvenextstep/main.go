package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"

	"proposalplatform/internal/database"
	"proposalplatform/internal/recipe"
)

// Input para resolver o próximo passo. Inclui proposta e steps atuais.
type Input struct {
	TenantID       string            `json:"tenantId"`
	ProposalID     string            `json:"proposalId"`
	JourneyType    string            `json:"journeyType"`
	JourneyVersion string            `json:"journeyVersion"`
	Proposal       database.Proposal `json:"proposal"`
	Steps          []database.Step   `json:"steps"`
	Recipe         recipe.Recipe     `json:"recipe"`
}

// StepDefinition descreve o step a ser executado.
type StepDefinition struct {
	Name           string                 `json:"name"`
	HookName       string                 `json:"hookName,omitempty"`
	TimeoutSeconds int                    `json:"timeoutSeconds,omitempty"`
	Input          map[string]interface{} `json:"input,omitempty"`
	Action         recipe.Action          `json:"action,omitempty"`
	Retry          recipe.Retry           `json:"retry,omitempty"`
}

// Output define a decisão do próximo passo.
type Output struct {
	Decision       string                 `json:"decision"` // ASYNC, SYNC, TERMINAL
	Step           *StepDefinition        `json:"step,omitempty"`
	Input          map[string]interface{} `json:"input,omitempty"`
	TerminalStatus string                 `json:"terminalStatus,omitempty"`
	ReasonCodes    []string               `json:"reasonCodes,omitempty"`
}

// handler determina o próximo passo a partir da receita YAML carregada pelo loadproposal.
func handler(ctx context.Context, in Input) (Output, error) {
	completed := make([]recipe.CompletedStep, 0, len(in.Steps))
	for _, s := range in.Steps {
		completed = append(completed, recipe.CompletedStep{
			Name:    s.Name,
			State:   s.State,
			Outcome: s.Outcome,
		})
	}

	decision, err := in.Recipe.Resolve(completed, in.Proposal.Context)
	if err != nil {
		return Output{}, err
	}
	out := Output{
		Decision:       decision.Type,
		TerminalStatus: decision.TerminalStatus,
		ReasonCodes:    decision.ReasonCodes,
	}
	if decision.Step != nil {
		out.Step = &StepDefinition{
			Name:           decision.Step.Name,
			HookName:       decision.Step.HookName,
			TimeoutSeconds: decision.Step.TimeoutSeconds,
			Input:          decision.Step.Input,
			Action:         decision.Step.Action,
			Retry:          decision.Step.Retry,
		}
		out.Input = decision.Step.Input
		if out.Input == nil {
			out.Input = map[string]interface{}{}
		}
	}
	return out, nil
}

func main() {
	lambda.Start(handler)
}
