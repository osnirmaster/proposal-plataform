package main

import (
    "context"
    "strings"

    "proposalplatform/internal/database"
    "github.com/aws/aws-lambda-go/lambda"
)

// Input para resolver o próximo passo. Inclui proposta e steps atuais.
type Input struct {
    TenantID       string            `json:"tenantId"`
    ProposalID     string            `json:"proposalId"`
    JourneyType    string            `json:"journeyType"`
    JourneyVersion string            `json:"journeyVersion"`
    Proposal       database.Proposal `json:"proposal"`
    Steps          []database.Step   `json:"steps"`
}

// StepDefinition descreve o step a ser executado.
type StepDefinition struct {
    Name          string                 `json:"name"`
    HookName      string                 `json:"hookName,omitempty"`
    TimeoutSeconds int                    `json:"timeoutSeconds,omitempty"`
}

// Output define a decisão do próximo passo.
type Output struct {
    Decision      string                 `json:"decision"` // ASYNC, SYNC, TERMINAL
    Step          *StepDefinition        `json:"step,omitempty"`
    Input         map[string]interface{} `json:"input,omitempty"`
    TerminalStatus string                `json:"terminalStatus,omitempty"`
    ReasonCodes   []string               `json:"reasonCodes,omitempty"`
}

// handler contém uma lógica de exemplo para determinar o próximo passo. Este código
// examina os steps concluídos e decide entre "KYC_CHECK" (assíncrono) e
// "CREATE_ACCOUNT" (síncrono). Se o KYC falhou ou foi rejeitado, encerra a proposta.
func handler(ctx context.Context, in Input) (Output, error) {
    // Procura se há step KYC_CHECK completo
    var kycOutcome string
    for _, s := range in.Steps {
        if strings.EqualFold(s.Name, "KYC_CHECK") {
            kycOutcome = s.Outcome
        }
    }

    // Se ainda não executamos o KYC, solicita step assíncrono
    if kycOutcome == "" {
        step := &StepDefinition{
            Name:          "KYC_CHECK",
            HookName:      "KYC",
            TimeoutSeconds: 300,
        }
        return Output{
            Decision: "ASYNC",
            Step:     step,
            Input:    map[string]interface{}{},
        }, nil
    }
    // Se KYC foi rejeitado, encerra como REJECTED
    if strings.EqualFold(kycOutcome, "REJECTED") {
        return Output{
            Decision:       "TERMINAL",
            TerminalStatus: "REJECTED",
            ReasonCodes:    []string{"KYC_REJECT"},
        }, nil
    }
    // Se KYC aprovado, executa step síncrono de criação de conta
    if strings.EqualFold(kycOutcome, "APPROVED") {
        step := &StepDefinition{
            Name: "CREATE_ACCOUNT",
        }
        return Output{
            Decision: "SYNC",
            Step:     step,
            Input:    map[string]interface{}{},
        }, nil
    }
    // Caso contrário, encerra como não suportado
    return Output{
        Decision:       "TERMINAL",
        TerminalStatus: "REJECTED",
        ReasonCodes:    []string{"UNSUPPORTED_STATE"},
    }, nil
}

func main() {
    lambda.Start(handler)
}