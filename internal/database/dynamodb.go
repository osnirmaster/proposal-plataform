package database

import (
    "context"
    "errors"
    "os"
    "time"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

    "fmt"
)

// Client encapsula a conexão com o DynamoDB e os nomes das tabelas.
type Client struct {
    db            *dynamodb.Client
    proposalsTbl  string
    stepsTbl      string
}

// New cria um novo cliente DynamoDB usando configurações padrão do AWS SDK.
// Caso a variável de ambiente DYNAMODB_ENDPOINT esteja definida, ela será usada
// para apontar o cliente para um endpoint local (por exemplo, LocalStack).
func New(ctx context.Context) (*Client, error) {
    region := os.Getenv("AWS_REGION")
    if region == "" {
        region = "us-east-1"
    }
    cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
    if err != nil {
        return nil, err
    }
    if endpoint := os.Getenv("DYNAMODB_ENDPOINT"); endpoint != "" {
        cfg.EndpointResolverWithOptions = dynamodb.EndpointResolverFromURL(endpoint)
    }
    db := dynamodb.NewFromConfig(cfg)
    proposalsTbl := os.Getenv("PROPOSALS_TABLE")
    if proposalsTbl == "" {
        proposalsTbl = "proposals"
    }
    stepsTbl := os.Getenv("STEPS_TABLE")
    if stepsTbl == "" {
        stepsTbl = "proposal_steps"
    }
    return &Client{db: db, proposalsTbl: proposalsTbl, stepsTbl: stepsTbl}, nil
}

// Proposal representa a estrutura simplificada de uma proposta.
type Proposal struct {
    TenantID   string                 `dynamodbav:"tenantId" json:"tenantId"`
    ProposalID string                 `dynamodbav:"proposalId" json:"proposalId"`
    Status     string                 `dynamodbav:"status" json:"status"`
    Context    map[string]interface{} `dynamodbav:"context" json:"context"`
    UpdatedAt  string                 `dynamodbav:"updatedAt" json:"updatedAt"`
}

// Step representa um step da proposta.
type Step struct {
    ProposalID    string                 `dynamodbav:"proposalId" json:"proposalId"`
    Name          string                 `dynamodbav:"name" json:"name"`
    Attempt       int64                  `dynamodbav:"attempt" json:"attempt"`
    State         string                 `dynamodbav:"state" json:"state"`
    Outcome       string                 `dynamodbav:"outcome" json:"outcome"`
    Result        map[string]interface{} `dynamodbav:"result" json:"result"`
    StartedAt     string                 `dynamodbav:"startedAt" json:"startedAt"`
    CompletedAt   string                 `dynamodbav:"completedAt" json:"completedAt"`
}

// GetProposal busca uma proposta pelo tenantId e proposalId.
func (c *Client) GetProposal(ctx context.Context, tenantID, proposalID string) (*Proposal, error) {
    pk := tenantID + "#" + proposalID
    out, err := c.db.GetItem(ctx, &dynamodb.GetItemInput{
        TableName: aws.String(c.proposalsTbl),
        Key: map[string]types.AttributeValue{
            "pk": &types.AttributeValueMemberS{Value: pk},
            "sk": &types.AttributeValueMemberS{Value: "META"},
        },
    })
    if err != nil {
        return nil, err
    }
    if out.Item == nil {
        return nil, errors.New("proposal not found")
    }
    var p Proposal
    if err := attributevalue.UnmarshalMap(out.Item, &p); err != nil {
        return nil, err
    }
    return &p, nil
}

// PutProposal persiste uma proposta. Utiliza UpdateItem para atualizar campos.
func (c *Client) PutProposal(ctx context.Context, p *Proposal) error {
    pk := p.TenantID + "#" + p.ProposalID
    now := time.Now().UTC().Format(time.RFC3339)
    p.UpdatedAt = now
    av, err := attributevalue.MarshalMap(p)
    if err != nil {
        return err
    }
    // Se o atributo sk não existir, adiciona como META
    av["pk"] = &types.AttributeValueMemberS{Value: pk}
    av["sk"] = &types.AttributeValueMemberS{Value: "META"}
    _, err = c.db.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String(c.proposalsTbl),
        Item:     av,
    })
    return err
}

// GetSteps retorna todos os steps de uma proposta ordenados por attempt.
func (c *Client) GetSteps(ctx context.Context, proposalID string) ([]Step, error) {
    out, err := c.db.Query(ctx, &dynamodb.QueryInput{
        TableName:              aws.String(c.stepsTbl),
        KeyConditionExpression: aws.String("pk = :pk"),
        ExpressionAttributeValues: map[string]types.AttributeValue{
            ":pk": &types.AttributeValueMemberS{Value: proposalID},
        },
    })
    if err != nil {
        return nil, err
    }
    var steps []Step
    if err := attributevalue.UnmarshalListOfMaps(out.Items, &steps); err != nil {
        return nil, err
    }
    return steps, nil
}

// PutStep persiste um step específico. Este exemplo grava ou substitui o item.
func (c *Client) PutStep(ctx context.Context, s *Step) error {
    av, err := attributevalue.MarshalMap(s)
    if err != nil {
        return err
    }
    av["pk"] = &types.AttributeValueMemberS{Value: s.ProposalID}
    sk := s.Name + "#" + fmt.Sprint(s.Attempt)
    av["sk"] = &types.AttributeValueMemberS{Value: sk}
    _, err = c.db.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String(c.stepsTbl),
        Item:     av,
    })
    return err
}