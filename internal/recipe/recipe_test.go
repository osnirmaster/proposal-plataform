package recipe

import "testing"

func TestResolveReturnsInitialStepWhenNoStepCompleted(t *testing.T) {
	r := testRecipe()

	decision, err := r.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if decision.Type != "ASYNC" {
		t.Fatalf("decision.Type = %q, want ASYNC", decision.Type)
	}
	if decision.Step == nil || decision.Step.Name != "KYC_CHECK" {
		t.Fatalf("decision.Step = %#v, want KYC_CHECK", decision.Step)
	}
}

func TestResolveReturnsNextStepAfterApprovedOutcome(t *testing.T) {
	r := testRecipe()

	decision, err := r.Resolve([]CompletedStep{{Name: "KYC_CHECK", Outcome: "APPROVED"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if decision.Type != "SYNC" {
		t.Fatalf("decision.Type = %q, want SYNC", decision.Type)
	}
	if decision.Step == nil || decision.Step.Name != "CREATE_ACCOUNT" {
		t.Fatalf("decision.Step = %#v, want CREATE_ACCOUNT", decision.Step)
	}
}

func TestResolveReturnsTerminalDecision(t *testing.T) {
	r := testRecipe()

	decision, err := r.Resolve([]CompletedStep{
		{Name: "KYC_CHECK", Outcome: "APPROVED"},
		{Name: "CREATE_ACCOUNT", Outcome: "APPROVED"},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if decision.Type != "TERMINAL" {
		t.Fatalf("decision.Type = %q, want TERMINAL", decision.Type)
	}
	if decision.TerminalStatus != "APPROVED" {
		t.Fatalf("decision.TerminalStatus = %q, want APPROVED", decision.TerminalStatus)
	}
}

func TestResolvePipelineUsesContextWhenExpression(t *testing.T) {
	r := testPipelineRecipe()

	decision, err := r.Resolve(nil, map[string]interface{}{
		"customer": map[string]interface{}{
			"submitted": true,
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if decision.Type != "ASYNC" {
		t.Fatalf("decision.Type = %q, want ASYNC", decision.Type)
	}
	if decision.Step == nil || decision.Step.Name != "KYC_CHECK" {
		t.Fatalf("decision.Step = %#v, want KYC_CHECK", decision.Step)
	}
}

func TestResolvePipelineReturnsIntegrationStep(t *testing.T) {
	r := testPipelineRecipe()

	decision, err := r.Resolve([]CompletedStep{
		{Name: "KYC_CHECK", State: "COMPLETED"},
	}, map[string]interface{}{
		"customer": map[string]interface{}{
			"submitted": true,
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if decision.Type != "SYNC" {
		t.Fatalf("decision.Type = %q, want SYNC", decision.Type)
	}
	if decision.Step == nil || decision.Step.Name != "CREATE_ACCOUNT" {
		t.Fatalf("decision.Step = %#v, want CREATE_ACCOUNT", decision.Step)
	}
	if decision.Step.Action.TypeDetails.Endpoint.URL != "http://localhost:8080/account/create" {
		t.Fatalf("endpoint url = %q, want resolved endpoint", decision.Step.Action.TypeDetails.Endpoint.URL)
	}
}

func testRecipe() Recipe {
	return Recipe{
		JourneyType:    "proposal",
		JourneyVersion: "v1",
		InitialStep:    "KYC_CHECK",
		Steps: []StepDefinition{
			{
				Name:      "KYC_CHECK",
				Execution: "ASYNC",
				Transitions: map[string]Transition{
					"APPROVED": {NextStep: "CREATE_ACCOUNT"},
					"REJECTED": {TerminalStatus: "REJECTED"},
				},
			},
			{
				Name:      "CREATE_ACCOUNT",
				Execution: "SYNC",
				Transitions: map[string]Transition{
					"APPROVED": {TerminalStatus: "APPROVED"},
				},
			},
		},
	}
}

func testPipelineRecipe() Recipe {
	return Recipe{
		JourneyType:    "ACCOUNT_OPENING",
		JourneyVersion: "2026-02-01",
		Endpoints: map[string]Endpoint{
			"service://core-banking/account/create": {
				URL:    "http://localhost:8080/account/create",
				Method: "POST",
			},
		},
		Pipeline: []PipelineStep{
			{
				Step: "KYC_CHECK",
				When: "context.customer.submitted == true",
				Action: Action{
					Type:     "EVENT_HOOK",
					HookName: "KYC",
				},
				TimeoutSeconds: 300,
			},
			{
				Step: "CREATE_ACCOUNT",
				When: "steps.KYC_CHECK == 'COMPLETED'",
				Action: Action{
					Type: "INTEGRATION",
					TypeDetails: TypeDetails{
						Mode:        "SYNC_HTTP",
						EndpointRef: "service://core-banking/account/create",
						RequestMapping: map[string]string{
							"customer_name": "$.context.person.nomeCompleto",
						},
						ResponseMapping: map[string]string{
							"accountId": "$.body.account_id",
						},
						ResponseTarget: "$.context.account",
					},
				},
			},
		},
	}
}
