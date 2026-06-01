package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"proposalplatform/internal/database"
	"proposalplatform/internal/mapping"
	"proposalplatform/internal/recipe"
)

// Input descreve a solicitação de execução assíncrona de um step.
type Input struct {
	TenantID    string                 `json:"tenantId"`
	ProposalID  string                 `json:"proposalId"`
	JourneyType string                 `json:"journeyType"`
	Step        map[string]interface{} `json:"step"`
	Input       map[string]interface{} `json:"input"`
	TaskToken   string                 `json:"taskToken"`
}

// Output é devolvido imediatamente para Step Functions, mas normalmente
// seria utilizado apenas após o retorno via callback.
type Output struct {
	Outcome string                 `json:"outcome"`
	Result  map[string]interface{} `json:"result"`
}

func handler(ctx context.Context, in Input) (Output, error) {
	// Conecta ao DynamoDB para registrar o step em execução
	db, err := database.New(ctx)
	if err != nil {
		return Output{}, err
	}
	stepDef, err := decodeStep(in.Step)
	if err != nil {
		return Output{}, err
	}
	requestPayload := in.Input
	if len(stepDef.Action.TypeDetails.RequestMapping) > 0 {
		proposal, err := db.GetProposal(ctx, in.TenantID, in.ProposalID)
		if err != nil {
			return Output{}, err
		}
		requestPayload, err = mapping.Apply(map[string]interface{}{
			"context": proposal.Context,
			"input":   in.Input,
		}, stepDef.Action.TypeDetails.RequestMapping)
		if err != nil {
			return Output{}, err
		}
	}

	step := &database.Step{
		ProposalID:  in.ProposalID,
		Name:        stepDef.Name,
		Attempt:     1,
		State:       "RUNNING",
		Outcome:     "",
		Result:      map[string]interface{}{"request": requestPayload},
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
		CompletedAt: "",
	}
	if err := db.PutStep(ctx, step); err != nil {
		log.Printf("erro ao persistir step: %v", err)
	}
	// Em um ambiente real, aqui seria publicado um evento no EventBridge contendo o taskToken.
	return Output{
		Outcome: "PENDING",
		Result: map[string]interface{}{
			"message":   "Step assíncrono iniciado",
			"request":   requestPayload,
			"taskToken": in.TaskToken,
			"hookName":  stepDef.HookName,
		},
	}, nil
}

func main() {
	lambda.Start(handler)
}

func decodeStep(raw map[string]interface{}) (recipe.StepExecution, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return recipe.StepExecution{}, err
	}
	var step recipe.StepExecution
	if err := json.Unmarshal(data, &step); err != nil {
		return recipe.StepExecution{}, err
	}
	return step, nil
}
