# Proposal Platform (Go)

Este repositório contém uma implementação de referência para uma plataforma de **propostas** inspirada no
exemplo discutido anteriormente. O foco desta implementação é demonstrar uma arquitetura
orientada a eventos e extensível para gerenciar jornadas de propostas de forma agnóstica,
utilizando **AWS Step Functions**, **AWS Lambda** (escritas em Go) e **Amazon DynamoDB** como
banco de dados de fonte de verdade.

> **Observação:** Esta implementação fornece um esqueleto funcional que pode ser executado
> localmente com apoio do [LocalStack](https://localstack.cloud/) via `docker‑compose`. Os
> handlers das funções Lambda são simplificados e servem de base para adaptar conforme
> as regras de negócio específicas de cada time.

## Estrutura do projeto

```
proposal-platform/
├── cmd/
│   ├── applystepresult/       # Lambda para consolidar o resultado de um step
│   ├── executesyncstep/       # Lambda para executar steps síncronos
│   ├── loadproposal/          # Lambda para carregar a proposta e passos atuais
│   ├── markterminal/          # Lambda para marcar proposta como terminal
│   ├── requestasyncstep/      # Lambda para solicitar execução de step assíncrono
│   └── resolvenextstep/       # Lambda para decidir o próximo step com base na jornada
├── internal/
│   └── database/
│       └── dynamodb.go        # Conexão e utilidades para DynamoDB
├── recipes/
│   └── proposal-v1.yaml       # Receita DSL/YAML da jornada de proposta
├── state_machine.asl.json     # Definição do State Machine (ASL) para Step Functions
├── docker-compose.yml         # Serviços locais (LocalStack com Lambda, DynamoDB, Step Functions)
└── README.md                  # Este documento
```

### Lambda Handlers

Cada handler recebe um JSON de entrada com as propriedades necessárias para a execução e
interage com o DynamoDB. Eles são escritos para funcionar tanto em ambiente AWS quanto
localmente (via LocalStack), desde que as variáveis de ambiente apropriadas estejam
definidas (e.g. `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` e
`DYNAMODB_ENDPOINT`).

* **loadproposal**: Carrega a proposta e seus steps a partir do DynamoDB.
* **resolvenextstep**: Determina qual será o próximo passo com base no estado atual da proposta.
* **executesyncstep**: Executa uma ação síncrona (simulada) e retorna o resultado imediatamente.
* **requestasyncstep**: Marca um step como em execução e publica um evento (em um ambiente real isso seria uma chamada ao Amazon EventBridge). Nesta implementação local, apenas grava o estado.
* **applystepresult**: Consolida o resultado de um step, persistindo o outcome e atualizando o status da proposta.
* **markterminal**: Define o status final da proposta (aprovada ou rejeitada).

### Receita DSL/YAML/JSON

A jornada da proposta pode ser definida em YAML ou JSON. O exemplo legado está em
`recipes/proposal-v1.yaml`, e o exemplo em formato CRD/DSL está em
`recipes/account_open.json`. O arquivo descreve:

* `journeyType` e `journeyVersion`: identificam qual receita será carregada.
* `initialStep`: primeiro step da jornada.
* `steps`: catálogo de etapas, informando se a execução é `SYNC` ou `ASYNC`.
* `transitions`: mapa de outcomes possíveis (`APPROVED`, `REJECTED`, `FAILED`, etc.)
  para o próximo step ou para um status terminal.
* `pipeline`: lista declarativa de steps com condição `when`, ação e timeout.
* `endpoints`: catálogo de endpoints externos referenciados por `endpointRef`.
* `requestMapping`: de/para para montar o payload enviado à integração.
* `responseMapping`: de/para para extrair campos da resposta.
* `responseTarget`: caminho do `proposal.context` onde o resultado mapeado será salvo.

Exemplo simplificado:

```yaml
journeyType: proposal
journeyVersion: v1
initialStep: KYC_CHECK

steps:
  - name: KYC_CHECK
    execution: ASYNC
    hookName: KYC
    timeoutSeconds: 300
    transitions:
      APPROVED:
        nextStep: CREATE_ACCOUNT
      REJECTED:
        terminalStatus: REJECTED
        reasonCodes:
          - KYC_REJECT
```

O `loadproposal` carrega a proposta, os steps atuais e a receita YAML. Em seguida,
o `resolvenextstep` usa essa receita para decidir o próximo step a executar ou o
status terminal da proposta.

Exemplo de integração HTTP no formato CRD/DSL:

```json
{
  "step": "CREATE_ACCOUNT",
  "when": "steps.SIGN_TERMS == 'COMPLETED'",
  "action": {
    "type": "INTEGRATION",
    "typeDetails": {
      "mode": "SYNC_HTTP",
      "endpointRef": "service://core-banking/account/create",
      "requestMapping": {
        "customer_name": "$.context.person.nomeCompleto",
        "document_id": "$.context.person.cpf",
        "package_id": "$.context.offer.packageId"
      },
      "responseMapping": {
        "accountId": "$.body.account_id",
        "branch": "$.body.branch",
        "creationDate": "$.body.created_at"
      },
      "responseTarget": "$.context.account"
    }
  }
}
```

Nesse fluxo, o `executeSyncStep` aplica o `requestMapping`, chama o endpoint
resolvido por `endpointRef`, aplica o `responseMapping` sobre a resposta HTTP e
devolve um `contextPatch`. O `applyStepResult` persiste esse patch no
`proposal.context`.

### State Machine (ASL)

A definição do `state_machine.asl.json` utiliza a Amazon States Language (ASL) para
orquestrar a execução das funções Lambda. Cada estado representa um passo do fluxo de
jornada, delegando a lógica de transição para as Lambdas. É possível importar esse
arquivo no [AWS Step Functions Workflow Studio](https://docs.aws.amazon.com/step-functions/latest/dg/procedure-creating-workflows.html)
para visualizar e editar o fluxo.

### Docker Compose

O `docker-compose.yml` fornecido neste repositório cria um ambiente local com
[LocalStack](https://github.com/localstack/localstack), permitindo simular os serviços
AWS utilizados (Lambda, DynamoDB e Step Functions). Após executar `docker‑compose up`,
os endpoints dos serviços ficam disponíveis em `http://localhost:4566`. Os handlers
podem ser invocados manualmente utilizando o AWS CLI ou ferramentas de sua escolha.

### Instruções de uso local

1. Certifique‑se de ter o [Docker](https://docs.docker.com/get-docker/) instalado.
2. Clone este repositório e acesse a pasta `proposal-platform`:

   ```bash
   git clone <repo-url>
   cd proposal-platform
   ```

3. Construa as dependências Go (opcional, pois a Lambda builder fará isso automaticamente):

   ```bash
   go mod download
   ```

4. Suba o ambiente local:

   ```bash
   docker-compose up
   ```

5. Exporte as variáveis de ambiente apropriadas para o AWS CLI se desejar testar via CLI:

   ```bash
   export AWS_ACCESS_KEY_ID=test
   export AWS_SECRET_ACCESS_KEY=test
   export AWS_REGION=us-east-1
   ```

   O LocalStack usa credenciais fictícias; portanto, qualquer valor é aceito.

6. Crie as tabelas DynamoDB e a máquina de estados utilizando comandos AWS CLI ou via
   um script de inicialização. O script de exemplo `scripts/init_db.sh` (não fornecido
   aqui) pode ser adaptado para criar as tabelas `proposals` e `proposal_steps`.

7. Utilize o Console do LocalStack ou o AWS CLI para iniciar execuções da Step Functions
   e testar as Lambdas.

## Próximos passos

Esta base oferece um ponto de partida. Para uma solução de produção recomenda‑se:

* Implementar validações de entrada mais robustas e tratamento de erros.
* Adicionar políticas de retry e timeouts em concordância com o catálogo de jornadas.
* Integrar com o Amazon EventBridge para disparo/consumo de eventos.
* Implementar controle de versão de jornadas e schemas de payloads.
* Proteger o acesso ao DynamoDB com políticas de IAM e KMS quando necessário.

Sinta‑se à vontade para adaptar e ampliar conforme as necessidades específicas do
produto ou time.
