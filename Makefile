SHELL := /bin/bash

AWS_ENDPOINT ?= http://localhost:4566
AWS_REGION ?= us-east-1
SM_NAME ?= proposal-platform-sm
EXEC_INPUT ?= {"tenantId":"tenant-001","proposalId":"prop-123","journeyType":"ACCOUNT_OPENING","journeyVersion":"2026-02-01"}

export AWS_ACCESS_KEY_ID ?= test
export AWS_SECRET_ACCESS_KEY ?= test
export AWS_DEFAULT_REGION ?= $(AWS_REGION)

.PHONY: local-up local-down local-logs local-setup local-status local-test local-list-exec

local-up:
	docker compose up -d localstack

local-down:
	docker compose down

local-logs:
	docker compose logs -f localstack

local-setup:
	./scripts/setup_localstack.sh

local-status:
	curl -fsS $(AWS_ENDPOINT)/_localstack/health | jq .

local-test:
	@SM_ARN="$$(aws --endpoint-url $(AWS_ENDPOINT) stepfunctions list-state-machines --query "stateMachines[?name=='$(SM_NAME)'].stateMachineArn" --output text)"; \
	if [[ -z "$$SM_ARN" || "$$SM_ARN" == "None" ]]; then \
	  echo "State Machine '$(SM_NAME)' nao encontrada. Rode: make local-setup"; \
	  exit 1; \
	fi; \
	aws --endpoint-url $(AWS_ENDPOINT) stepfunctions start-execution \
	  --state-machine-arn "$$SM_ARN" \
	  --name "exec-$$(date +%s)" \
	  --input '$(EXEC_INPUT)'

local-list-exec:
	@SM_ARN="$$(aws --endpoint-url $(AWS_ENDPOINT) stepfunctions list-state-machines --query "stateMachines[?name=='$(SM_NAME)'].stateMachineArn" --output text)"; \
	if [[ -z "$$SM_ARN" || "$$SM_ARN" == "None" ]]; then \
	  echo "State Machine '$(SM_NAME)' nao encontrada. Rode: make local-setup"; \
	  exit 1; \
	fi; \
	aws --endpoint-url $(AWS_ENDPOINT) stepfunctions list-executions --state-machine-arn "$$SM_ARN"
