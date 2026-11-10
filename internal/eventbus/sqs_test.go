package eventbus

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	qt "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestSQSRefusesMisroutedQueues(t *testing.T) {
	for _, url := range []string{"http://sqs.us-east-1.amazonaws.com/123456789012/test", "https://sqs.eu-west-1.amazonaws.com/123456789012/test", "https://private@attacker.invalid/123456789012/test", "https://sqs.us-east-1.amazonaws.com/123456789012/bad/path", "https://sqs.us-east-1.amazonaws.com/123456789012/test?x=private"} {
		if _, e := NewSQS(context.Background(), url, "us-east-1"); e == nil || strings.Contains(e.Error(), "private") {
			t.Fatal("bad configuration accepted or leaked")
		}
	}
}
func TestIntegrationSQSFIFOReplayAndRejection(t *testing.T) {
	endpoint := integrationEnv(t, "INTEGRATION_SQS_ENDPOINT")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := sqs.New(sqs.Options{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("fixture", "fixture", ""), BaseEndpoint: aws.String(endpoint), RetryMaxAttempts: 1})
	name := "wppgo-" + uuid.NewString() + ".fifo"
	var q *sqs.CreateQueueOutput
	var err error
	for i := 0; i < 10; i++ {
		q, err = client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: &name, Attributes: map[string]string{"FifoQueue": "true"}})
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: q.QueueUrl}); err != nil {
			t.Error(err)
		}
	}()
	e := Event{ID: uuid.NewString(), Session: "alpha", Name: "onmessage", Data: []byte(`{"event":"onmessage","session":"alpha"}`)}
	target := SQS{client: client, queue: aws.ToString(q.QueueUrl), fifo: true}
	bad := target
	bad.queue = strings.TrimSuffix(target.queue, name) + "missing-" + name
	if err := bad.Deliver(ctx, e); err == nil {
		t.Fatal("nonexistent queue accepted")
	}
	for i := 0; i < 2; i++ {
		if err := target.Deliver(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	m, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: q.QueueUrl, MessageAttributeNames: []string{"All"}, MessageSystemAttributeNames: []qt.MessageSystemAttributeName{"All"}, WaitTimeSeconds: 2, MaxNumberOfMessages: 10})
	if err != nil || len(m.Messages) != 1 {
		t.Fatal("expected one FIFO message", err)
	}
	item := m.Messages[0]
	if aws.ToString(item.Body) != string(e.Data) || aws.ToString(item.MessageAttributes["eventId"].StringValue) != e.ID || item.Attributes["MessageGroupId"] != "alpha" {
		t.Fatal("event identity or body changed")
	}
	if _, err = client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: q.QueueUrl, ReceiptHandle: item.ReceiptHandle}); err != nil {
		t.Fatal(err)
	}
	m, err = client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: q.QueueUrl, WaitTimeSeconds: 1})
	if err != nil || len(m.Messages) != 0 {
		t.Fatal("duplicate remained after first was consumed", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if !errors.Is(target.Deliver(canceled, e), context.Canceled) {
		t.Fatal("ignored cancellation")
	}
}
