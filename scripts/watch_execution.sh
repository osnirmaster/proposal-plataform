#!/usr/bin/env bash
set -euo pipefail

AWS_ENDPOINT="${AWS_ENDPOINT:-http://localhost:4566}"
AWS_REGION="${AWS_REGION:-us-east-1}"
SM_NAME="${SM_NAME:-proposal-platform-sm}"
REFRESH_SECONDS="${REFRESH_SECONDS:-2}"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="$AWS_REGION"

aws_local() {
  aws --endpoint-url "$AWS_ENDPOINT" "$@"
}

get_state_machine_arn() {
  aws_local stepfunctions list-state-machines \
    --query "stateMachines[?name=='$SM_NAME'].stateMachineArn | [0]" \
    --output text
}

get_latest_execution_arn() {
  local sm_arn="$1"
  aws_local stepfunctions list-executions \
    --state-machine-arn "$sm_arn" \
    --max-results 1 \
    --query "executions[0].executionArn" \
    --output text
}

render_header() {
  local execution_arn="$1"
  local status="$2"
  local start_date="$3"
  local stop_date="$4"
  local error="$5"
  local cause="$6"

  printf "Proposal Platform :: local-watch\n"
  printf "State Machine : %s\n" "$SM_NAME"
  printf "Execution ARN : %s\n" "$execution_arn"
  printf "Status        : %s\n" "$status"
  printf "Started       : %s\n" "$start_date"
  printf "Stopped       : %s\n" "${stop_date:-N/A}"
  if [[ "$error" != "null" && -n "$error" ]]; then
    printf "Error         : %s\n" "$error"
  fi
  if [[ "$cause" != "null" && -n "$cause" ]]; then
    printf "Cause         : %.220s\n" "$cause"
  fi
  printf "\n"
}

phase_from_state() {
  local state_name="$1"
  case "$state_name" in
    LoadProposal|ResolveNextStep) echo "Fase 0/1 - Preparacao e decisao" ;;
    RequestAsyncStep|HandleAsyncTimeout|HandleAsyncFailure) echo "Fase 1/2 - Fluxo assincrono" ;;
    ExecuteSyncStep) echo "Fase 3 - Integracao sincrona" ;;
    ApplyStepResult|CheckIfDone) echo "Fase 2 - Persistencia e consolidacao" ;;
    MarkAppliedTerminal|MarkResolvedTerminal|MarkTerminal) echo "Fase 4 - Encerramento" ;;
    *) echo "Fase desconhecida" ;;
  esac
}

render_timeline() {
  local history_json="$1"
  local timeline
  timeline="$(echo "$history_json" | jq -r '
    .events
    | sort_by(.id)
    | map(
        if .type=="TaskStateEntered" then
          "- ENTER  :: \(.stateEnteredEventDetails.name)"
        elif .type=="TaskStateExited" then
          "- EXIT   :: \(.stateExitedEventDetails.name)"
        elif .type=="ExecutionFailed" then
          "- FAILED :: \(.executionFailedEventDetails.error)"
        elif .type=="ExecutionSucceeded" then
          "- DONE   :: ExecutionSucceeded"
        else empty end
      )
    | .[-12:]
    | .[]
  ')"

  printf "Timeline (ultimos eventos)\n"
  if [[ -z "$timeline" ]]; then
    printf "- sem eventos ainda\n"
  else
    printf "%s\n" "$timeline"
  fi
  printf "\n"
}

main() {
  local sm_arn
  sm_arn="$(get_state_machine_arn)"
  if [[ -z "$sm_arn" || "$sm_arn" == "None" ]]; then
    echo "State Machine '$SM_NAME' nao encontrada. Rode: make local-setup"
    exit 1
  fi

  local execution_arn
  execution_arn="$(get_latest_execution_arn "$sm_arn")"
  if [[ -z "$execution_arn" || "$execution_arn" == "None" ]]; then
    echo "Nenhuma execucao encontrada. Rode: make local-test"
    exit 1
  fi

  while true; do
    local desc_json status start_date stop_date error cause history_json last_state phase
    desc_json="$(aws_local stepfunctions describe-execution --execution-arn "$execution_arn")"
    history_json="$(aws_local stepfunctions get-execution-history --execution-arn "$execution_arn" --max-results 200)"

    status="$(echo "$desc_json" | jq -r '.status')"
    start_date="$(echo "$desc_json" | jq -r '.startDate')"
    stop_date="$(echo "$desc_json" | jq -r '.stopDate // empty')"
    error="$(echo "$desc_json" | jq -r '.error')"
    cause="$(echo "$desc_json" | jq -r '.cause')"
    last_state="$(echo "$history_json" | jq -r '[.events[] | select(.type=="TaskStateEntered") | .stateEnteredEventDetails.name] | last // "N/A"')"
    phase="$(phase_from_state "$last_state")"

    if [[ -n "${TERM:-}" ]]; then
      clear || true
    else
      printf "\033c"
    fi
    render_header "$execution_arn" "$status" "$start_date" "$stop_date" "$error" "$cause"
    printf "Representacao\n"
    printf "[LoadProposal] -> [ResolveNextStep] -> [RequestAsyncStep | ExecuteSyncStep] -> [ApplyStepResult] -> [MarkTerminal]\n"
    printf "Estado atual : %s\n" "$last_state"
    printf "Fase         : %s\n\n" "$phase"
    render_timeline "$history_json"

    if [[ "$status" == "SUCCEEDED" || "$status" == "FAILED" || "$status" == "TIMED_OUT" || "$status" == "ABORTED" ]]; then
      printf "Execucao finalizada com status %s\n" "$status"
      break
    fi

    sleep "$REFRESH_SECONDS"
  done
}

main "$@"
