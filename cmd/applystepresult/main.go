package main

import (
    "context"
    "strings"
    "time"

    "proposalplatform/internal/database"
    "github.com/aws/aws-lambda-go/lambda"
)

// Input para consolidar o resultado de um step.
type Input struct {
    TenantID    string                 `json:"tenantId"`
    ProposalID  string                 `json:"proposalId"`
    JourneyType string                 `json:"journeyType"`
    Step        map[string]interface{} `json:"step"`
    Outcome     string                 `json:"outcome"`
    Result      map[string]interface{} `json:"result"`
}

// Output informa se o fluxo é terminal.
type Output struct {
    IsTerminal    bool     `json:"isTerminal"`
    TerminalStatus string  `json:"terminalStatus,omitempty"`
    ReasonCodes    []string `json:"reasonCodes,omitempty"`
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
    // Carrega a proposta
    prop, err := db.GetProposal(ctx, in.TenantID, in.ProposalID)
    if err != nil {
        return Output{}, err
    }
    // Lógica de transição simples
    outcome := strings.ToUpper(in.Outcome)
    var isTerminal bool
    var terminalStatus string
    var reasonCodes []string
    switch strings.ToUpper(name) {
    case "KYC_CHECK":
        if outcome == "REJECTED" {
            isTerminal = true
            terminalStatus = "REJECTED"
            reasonCodes = []string{"KYC_REJECT"}
        } else if outcome == "APPROVED" {
            // mantém proposta aberta para próximo passo
            prop.Status = "KYC_APPROVED"
        }
    case "CREATE_ACCOUNT":
        if outcome == "APPROVED" {
            isTerminal = true
            terminalStatus = "APPROVED"
        } else {
            isTerminal = true
            terminalStatus = "REJECTED"
        }
    default:
        // Caso desconhecido, encerra
        isTerminal = true
        terminalStatus = "REJECTED"
        reasonCodes = []string{"UNKNOWN_STEP"}
    }
    // Atualiza status na proposta
    if isTerminal {
        prop.Status = terminalStatus
    }
    if err := db.PutProposal(ctx, prop); err != nil {
        return Output{}, err
    }
    return Output{
        IsTerminal:    isTerminal,
        TerminalStatus: terminalStatus,
        ReasonCodes:    reasonCodes,
    }, nil
}

func main() {
    lambda.Start(handler)
}