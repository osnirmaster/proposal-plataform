package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"

	"proposalplatform/internal/database"
	"proposalplatform/internal/mapping"
	"proposalplatform/internal/recipe"
)

// Input descreve a solicitação de execução de step síncrono.
type Input struct {
	TenantID       string                 `json:"tenantId"`
	ProposalID     string                 `json:"proposalId"`
	JourneyType    string                 `json:"journeyType"`
	JourneyVersion string                 `json:"journeyVersion"`
	Step           map[string]interface{} `json:"step"`
	Input          map[string]interface{} `json:"input"`
}

// Output descreve o resultado do step síncrono.
type Output struct {
	Outcome string                 `json:"outcome"`
	Result  map[string]interface{} `json:"result"`
}

// handler executa um step síncrono. Quando o step declara uma integração HTTP,
// aplica requestMapping, chama o endpoint e aplica responseMapping no retorno.
func handler(ctx context.Context, in Input) (Output, error) {
	step, err := decodeStep(in.Step)
	if err != nil {
		return Output{}, err
	}
	if !strings.EqualFold(step.Action.Type, "INTEGRATION") {
		return Output{
			Outcome: "APPROVED",
			Result: map[string]interface{}{
				"message": "Step executado com sucesso",
			},
		}, nil
	}
	if !strings.EqualFold(step.Action.TypeDetails.Mode, "SYNC_HTTP") {
		return Output{}, fmt.Errorf("unsupported integration mode %q", step.Action.TypeDetails.Mode)
	}

	db, err := database.New(ctx)
	if err != nil {
		return Output{}, err
	}
	proposal, err := db.GetProposal(ctx, in.TenantID, in.ProposalID)
	if err != nil {
		return Output{}, err
	}
	if step.ProposalStatusOnStart != "" {
		journeyRecipe, err := recipe.Load(in.JourneyType, in.JourneyVersion)
		if err != nil {
			return Output{}, err
		}
		if !journeyRecipe.CanTransition(proposal.Status, step.ProposalStatusOnStart) {
			return Output{}, fmt.Errorf("invalid proposal status transition: %s -> %s", proposal.Status, step.ProposalStatusOnStart)
		}
		proposal.Status = step.ProposalStatusOnStart
		if err := db.PutProposal(ctx, proposal); err != nil {
			return Output{}, err
		}
	}

	source := map[string]interface{}{
		"context": proposal.Context,
		"input":   in.Input,
	}
	requestPayload, err := mapping.Apply(source, step.Action.TypeDetails.RequestMapping)
	if err != nil {
		return Output{}, err
	}
	responseBody, statusCode, err := callHTTP(ctx, step.Action.TypeDetails.Endpoint, requestPayload)
	if err != nil {
		return Output{
			Outcome: "FAILED",
			Result: map[string]interface{}{
				"error":   err.Error(),
				"request": requestPayload,
			},
		}, nil
	}

	responseSource := map[string]interface{}{
		"body": responseBody,
	}
	mappedResponse, err := mapping.Apply(responseSource, step.Action.TypeDetails.ResponseMapping)
	if err != nil {
		return Output{}, err
	}

	contextPatch := map[string]interface{}{}
	target := step.Action.TypeDetails.ResponseTarget
	if target == "" {
		target = defaultResponseTarget(step.Name)
	}
	if err := mapping.MergeAtPath(contextPatch, target, mappedResponse); err != nil {
		return Output{}, err
	}

	outcome := "APPROVED"
	if statusCode < 200 || statusCode >= 300 {
		outcome = "REJECTED"
	}
	return Output{
		Outcome: outcome,
		Result: map[string]interface{}{
			"request":        requestPayload,
			"response":       responseBody,
			"mappedResponse": mappedResponse,
			"contextPatch":   contextPatch,
			"statusCode":     statusCode,
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

func callHTTP(ctx context.Context, endpoint recipe.Endpoint, payload map[string]interface{}) (map[string]interface{}, int, error) {
	if endpoint.URL == "" {
		return nil, 0, fmt.Errorf("endpoint url is required")
	}
	method := endpoint.Method
	if method == "" {
		method = http.MethodPost
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range endpoint.Headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var responseBody map[string]interface{}
	if len(responseBytes) > 0 {
		if err := json.Unmarshal(responseBytes, &responseBody); err != nil {
			return nil, resp.StatusCode, err
		}
	}
	if responseBody == nil {
		responseBody = map[string]interface{}{}
	}
	return responseBody, resp.StatusCode, nil
}

func defaultResponseTarget(stepName string) string {
	stepName = strings.TrimSuffix(strings.ToLower(stepName), "_account")
	switch strings.ToUpper(stepName) {
	case "CREATE":
		return "$.context.account"
	default:
		return "$.context." + strings.ToLower(strings.ReplaceAll(stepName, "_", ""))
	}
}
