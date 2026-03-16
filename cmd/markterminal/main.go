package main

import (
    "context"

    "proposalplatform/internal/database"
    "github.com/aws/aws-lambda-go/lambda"
)

// Input para marcar a proposta como terminal.
type Input struct {
    TenantID       string   `json:"tenantId"`
    ProposalID     string   `json:"proposalId"`
    JourneyType    string   `json:"journeyType"`
    TerminalStatus string   `json:"terminalStatus"`
    ReasonCodes    []string `json:"reasonCodes"`
}

// Output retorna a proposta atualizada.
type Output struct {
    Proposal *database.Proposal `json:"proposal"`
}

func handler(ctx context.Context, in Input) (Output, error) {
    db, err := database.New(ctx)
    if err != nil {
        return Output{}, err
    }
    prop, err := db.GetProposal(ctx, in.TenantID, in.ProposalID)
    if err != nil {
        return Output{}, err
    }
    prop.Status = in.TerminalStatus
    if err := db.PutProposal(ctx, prop); err != nil {
        return Output{}, err
    }
    return Output{Proposal: prop}, nil
}

func main() {
    lambda.Start(handler)
}