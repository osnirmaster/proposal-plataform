package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"

	"proposalplatform/internal/database"
	"proposalplatform/internal/recipe"
)

// Input para a função loadproposal.
type Input struct {
	TenantID       string `json:"tenantId"`
	ProposalID     string `json:"proposalId"`
	JourneyType    string `json:"journeyType"`
	JourneyVersion string `json:"journeyVersion"`
}

// Output da função loadproposal.
type Output struct {
	Proposal *database.Proposal `json:"proposal"`
	Steps    []database.Step    `json:"steps"`
	Recipe   *recipe.Recipe     `json:"recipe"`
}

func handler(ctx context.Context, in Input) (Output, error) {
	db, err := database.New(ctx)
	if err != nil {
		log.Printf("error creating db client: %v", err)
		return Output{}, err
	}
	prop, err := db.GetProposal(ctx, in.TenantID, in.ProposalID)
	if err != nil {
		return Output{}, err
	}
	steps, err := db.GetSteps(ctx, in.ProposalID)
	if err != nil {
		// não é fatal, apenas retorna lista vazia
		steps = []database.Step{}
	}
	journeyRecipe, err := recipe.Load(in.JourneyType, in.JourneyVersion)
	if err != nil {
		return Output{}, err
	}
	return Output{Proposal: prop, Steps: steps, Recipe: journeyRecipe}, nil
}

func main() {
	lambda.Start(handler)
}
