package main

import (
    "context"

    "github.com/aws/aws-lambda-go/lambda"
)

// Input descreve a solicitação de execução de step síncrono.
type Input struct {
    TenantID    string                 `json:"tenantId"`
    ProposalID  string                 `json:"proposalId"`
    JourneyType string                 `json:"journeyType"`
    Step        map[string]interface{} `json:"step"`
    Input       map[string]interface{} `json:"input"`
}

// Output descreve o resultado do step síncrono.
type Output struct {
    Outcome string                 `json:"outcome"`
    Result  map[string]interface{} `json:"result"`
}

// handler executa um step síncrono. Este exemplo apenas simula a aprovação.
func handler(ctx context.Context, in Input) (Output, error) {
    return Output{
        Outcome: "APPROVED",
        Result: map[string]interface{}{
            "message": "Step executado com sucesso",
        },
    }, nil
}

func main() {
    lambda.Start(handler)
}