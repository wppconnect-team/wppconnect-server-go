package eventbus

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	qt "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"net/url"
	"regexp"
	"strings"
)

var regionName = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]+)+-[0-9]+$`)
var queuePath = regexp.MustCompile(`^/[0-9]{12}/[A-Za-z0-9_-]+(\.fifo)?$`)

type SQS struct {
	client *sqs.Client
	queue  string
	fifo   bool
}

func NewSQS(ctx context.Context, queue, region string) (*SQS, error) {
	u, e := url.Parse(queue)
	if e != nil || !regionName.MatchString(region) || u.Scheme != "https" || u.Host != "sqs."+region+".amazonaws.com" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !queuePath.MatchString(u.Path) || len(u.Path)-14 > 80 {
		return nil, errors.New("invalid AWS SQS queue URL or region")
	}
	cfg, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region), awsconfig.WithRetryMaxAttempts(1))
	if e != nil {
		return nil, errors.New("cannot initialize AWS SQS credentials/configuration")
	}
	// Explicit regional endpoint: never redirect signed queue requests to an endpoint selected by a caller.
	client := sqs.NewFromConfig(cfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String("https://" + u.Host) })
	return &SQS{client: client, queue: queue, fifo: strings.HasSuffix(u.Path, ".fifo")}, nil
}
func (t *SQS) Name() string { return "sqs" }
func (t *SQS) Close() error { return nil }
func (t *SQS) Deliver(ctx context.Context, e Event) error {
	e = externalIntegrationEvent(e)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(e.Data) == 0 || len(e.Data) > 1024*1024-2048 {
		return errors.New("event exceeds SQS payload bounds")
	}
	fields := map[string]qt.MessageAttributeValue{}
	for k, v := range map[string]string{"eventId": e.ID, "session": e.Session, "event": e.Name} {
		fields[k] = qt.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(v)}
	}
	in := &sqs.SendMessageInput{QueueUrl: aws.String(t.queue), MessageBody: aws.String(string(e.Data)), MessageAttributes: fields}
	if t.fifo {
		in.MessageDeduplicationId = aws.String(e.ID)
		in.MessageGroupId = aws.String(e.Session)
	}
	result, err := t.client.SendMessage(ctx, in)
	if err != nil || result == nil || aws.ToString(result.MessageId) == "" {
		return errors.New("SQS publish was not confirmed")
	}
	return nil
}
