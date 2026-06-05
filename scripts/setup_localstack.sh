#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

AWS_ENDPOINT="${AWS_ENDPOINT:-http://localhost:4566}"
LAMBDA_AWS_ENDPOINT="${LAMBDA_AWS_ENDPOINT:-http://host.docker.internal:4566}"
AWS_REGION="${AWS_REGION:-us-east-1}"
ACCOUNT_ID="${ACCOUNT_ID:-000000000000}"
ROLE_ARN="${ROLE_ARN:-arn:aws:iam::000000000000:role/lambda-execution-role}"
TMP_DIR="${TMP_DIR:-.tmp/localstack}"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="$AWS_REGION"

LAMBDA_NAMES=(
  "loadproposal"
  "resolvenextstep"
  "executesyncstep"
  "requestasyncstep"
  "applystepresult"
  "markterminal"
)

log() {
  printf '\n[%s] %s\n' "$(date +%H:%M:%S)" "$1"
}

aws_local() {
  aws --endpoint-url "$AWS_ENDPOINT" "$@"
}

ensure_localstack_up() {
  log "Subindo LocalStack com Docker Compose"
  docker compose up -d localstack
  log "Aguardando endpoint do LocalStack em $AWS_ENDPOINT"
  for _ in $(seq 1 60); do
    if curl -fsS "$AWS_ENDPOINT/_localstack/health" >/dev/null 2>&1; then
      log "LocalStack pronto"
      return
    fi
    sleep 2
  done
  echo "LocalStack nao respondeu a tempo" >&2
  exit 1
}

ensure_table() {
  local table_name="$1"

  if aws_local dynamodb describe-table --table-name "$table_name" >/dev/null 2>&1; then
    log "Tabela $table_name ja existe"
    return
  fi

  log "Criando tabela $table_name"
  if [[ "$table_name" == "proposals" ]]; then
    aws_local dynamodb create-table \
      --table-name "$table_name" \
      --attribute-definitions AttributeName=pk,AttributeType=S AttributeName=sk,AttributeType=S \
      --key-schema AttributeName=pk,KeyType=HASH AttributeName=sk,KeyType=RANGE \
      --billing-mode PAY_PER_REQUEST >/dev/null
    return
  fi

  if [[ "$table_name" == "proposal_steps" ]]; then
    aws_local dynamodb create-table \
      --table-name "$table_name" \
      --attribute-definitions AttributeName=pk,AttributeType=S AttributeName=sk,AttributeType=S \
      --key-schema AttributeName=pk,KeyType=HASH AttributeName=sk,KeyType=RANGE \
      --billing-mode PAY_PER_REQUEST >/dev/null
    return
  fi

  echo "Tabela nao suportada no setup: $table_name" >&2
  exit 1
}

build_lambda_zip() {
  local fn_name="$1"
  local src_dir="cmd/$fn_name"
  local out_dir="$TMP_DIR/$fn_name"

  rm -rf "$out_dir"
  mkdir -p "$out_dir"

  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$out_dir/bootstrap" "./$src_dir"
  cp -R recipes "$out_dir/recipes"
  (cd "$out_dir" && zip -qr "../$fn_name.zip" bootstrap recipes)
}

create_or_update_lambda() {
  local fn_name="$1"
  local zip_path="$TMP_DIR/$fn_name.zip"

  if aws_local lambda get-function --function-name "$fn_name" >/dev/null 2>&1; then
    log "Atualizando Lambda $fn_name"
    aws_local lambda update-function-code \
      --function-name "$fn_name" \
      --zip-file "fileb://$zip_path" >/dev/null
  else
    log "Criando Lambda $fn_name"
    aws_local lambda create-function \
      --function-name "$fn_name" \
      --runtime provided.al2 \
      --role "$ROLE_ARN" \
      --handler bootstrap \
      --zip-file "fileb://$zip_path" \
      --timeout 30 \
      --memory-size 256 \
      --environment "Variables={DYNAMODB_ENDPOINT=$LAMBDA_AWS_ENDPOINT,PROPOSALS_TABLE=proposals,STEPS_TABLE=proposal_steps,RECIPE_DIR=recipes,AWS_REGION=$AWS_REGION}" >/dev/null
    return
  fi

  aws_local lambda update-function-configuration \
    --function-name "$fn_name" \
    --runtime provided.al2 \
    --handler bootstrap \
    --timeout 30 \
    --memory-size 256 \
    --environment "Variables={DYNAMODB_ENDPOINT=$LAMBDA_AWS_ENDPOINT,PROPOSALS_TABLE=proposals,STEPS_TABLE=proposal_steps,RECIPE_DIR=recipes,AWS_REGION=$AWS_REGION}" >/dev/null
}

create_or_update_state_machine() {
  local sm_name="proposal-platform-sm"
  local definition_file="$ROOT_DIR/state_machine.asl.json"
  local sm_arn

  sm_arn="$(aws_local stepfunctions list-state-machines --query "stateMachines[?name=='$sm_name'].stateMachineArn" --output text)"
  if [[ -n "$sm_arn" && "$sm_arn" != "None" ]]; then
    log "Atualizando State Machine $sm_name"
    aws_local stepfunctions update-state-machine \
      --state-machine-arn "$sm_arn" \
      --definition "file://$definition_file" >/dev/null
    echo "$sm_arn"
    return
  fi

  log "Criando State Machine $sm_name"
  sm_arn="$(aws_local stepfunctions create-state-machine \
    --name "$sm_name" \
    --definition "file://$definition_file" \
    --role-arn "$ROLE_ARN" \
    --query stateMachineArn --output text)"
  echo "$sm_arn"
}

seed_sample_proposal() {
  local tenant="tenant-001"
  local proposal="prop-123"
  local now
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  log "Limpando steps antigos da proposta de exemplo ($proposal)"
  local step_keys
  step_keys="$(aws_local dynamodb query \
    --table-name proposal_steps \
    --key-condition-expression "pk = :pk" \
    --expression-attribute-values "{\":pk\":{\"S\":\"$proposal\"}}" \
    --query "Items[].{pk:pk.S,sk:sk.S}" \
    --output json)"
  echo "$step_keys" | jq -c '.[]' | while read -r key; do
    local pk sk
    pk="$(echo "$key" | jq -r '.pk')"
    sk="$(echo "$key" | jq -r '.sk')"
    aws_local dynamodb delete-item \
      --table-name proposal_steps \
      --key "{\"pk\":{\"S\":\"$pk\"},\"sk\":{\"S\":\"$sk\"}}" >/dev/null
  done

  log "Inserindo proposta de exemplo ($tenant / $proposal)"
  aws_local dynamodb put-item \
    --table-name proposals \
    --item "{
      \"pk\": {\"S\": \"$tenant#$proposal\"},
      \"sk\": {\"S\": \"META\"},
      \"tenantId\": {\"S\": \"$tenant\"},
      \"proposalId\": {\"S\": \"$proposal\"},
      \"status\": {\"S\": \"IN_PROGRESS\"},
      \"context\": {\"M\": {
        \"proposalId\": {\"S\": \"$proposal\"},
        \"channel\": {\"S\": \"APP\"},
        \"customer\": {\"M\": {\"submitted\": {\"BOOL\": true}}},
        \"person\": {\"M\": {
          \"nomeCompleto\": {\"S\": \"Maria Souza\"},
          \"cpf\": {\"S\": \"12345678901\"},
          \"dataNascimento\": {\"S\": \"1995-08-10\"}
        }},
        \"offer\": {\"M\": {
          \"packageId\": {\"S\": \"PKG-01\"},
          \"termsVersion\": {\"S\": \"v5\"}
        }}
      }},
      \"updatedAt\": {\"S\": \"$now\"}
    }" >/dev/null
}

main() {
  mkdir -p "$TMP_DIR"
  ensure_localstack_up

  ensure_table "proposals"
  ensure_table "proposal_steps"

  log "Empacotando Lambdas"
  for fn in "${LAMBDA_NAMES[@]}"; do
    build_lambda_zip "$fn"
    create_or_update_lambda "$fn"
  done

  sm_arn="$(create_or_update_state_machine)"
  seed_sample_proposal

  log "Ambiente pronto"
  cat <<EOF

Resumo:
- Endpoint LocalStack: $AWS_ENDPOINT
- Endpoint visto pelas Lambdas: $LAMBDA_AWS_ENDPOINT
- Regiao: $AWS_REGION
- State Machine ARN: $sm_arn

Para iniciar execucao de teste:
aws --endpoint-url $AWS_ENDPOINT stepfunctions start-execution \
  --state-machine-arn "$sm_arn" \
  --name "exec-$(date +%s)" \
  --input '{"tenantId":"tenant-001","proposalId":"prop-123","journeyType":"ACCOUNT_OPENING","journeyVersion":"2026-02-01"}'

Para ver execucoes:
aws --endpoint-url $AWS_ENDPOINT stepfunctions list-executions --state-machine-arn "$sm_arn"

EOF
}

main "$@"
