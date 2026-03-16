package main

import (
    "context"
    "log"
    "time"

    "proposalplatform/internal/database"
    "github.com/aws/aws-lambda-go/lambda"
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
    nameAny := in.Step["name"]
    nameStr, _ := nameAny.(string)
    step := &database.Step{
        ProposalID:  in.ProposalID,
        Name:        nameStr,
        Attempt:     1,
        State:       "RUNNING",
        Outcome:     "",
        Result:      map[string]interface{}{},
        StartedAt:   time.Now().UTC().Format(time.RFC3339),
        CompletedAt: "",
    }
    if err := db.PutStep(ctx, step); err != nil {
        log.Printf("erro ao persistir step: %v", err)
    }
    // Em um ambiente real, aqui seria publicado um evento no EventBridge contendo o taskToken
    return Output{
        Outcome: "PENDING",
        Result: map[string]interface{}{
            "message": "Step assíncrono iniciado",
        },
    }, nil
}

func main() {
    lambda.Start(handler)
}