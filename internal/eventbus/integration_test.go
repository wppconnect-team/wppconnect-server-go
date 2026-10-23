package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	amqp "github.com/rabbitmq/amqp091-go"
)

func integrationEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("INTEGRATION_REQUIRED") == "1" {
			t.Fatalf("missing %s", key)
		}
		t.Skip("set " + key + " to run integration fixtures")
	}
	return value
}
func TestIntegrationNATSPublishAndReconnect(t *testing.T) {
	address := integrationEnv(t, "INTEGRATION_NATS_URL")
	connection, err := nats.Connect(address, nats.Timeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	subject := "wppgo.integration." + strings.ReplaceAll(uuid.NewString(), "-", "")
	subscription, err := connection.SubscribeSync(subject + ".>")
	if err != nil {
		t.Fatal(err)
	}
	if err = connection.Flush(); err != nil {
		t.Fatal(err)
	}
	target := &NATS{URL: address, Subject: subject}
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	event := Event{ID: uuid.NewString(), Session: "alpha", Name: "onmessage", Data: json.RawMessage(`{"event":"onmessage","session":"alpha","body":"integration"}`)}
	for i := 0; i < 2; i++ {
		if err = target.Deliver(ctx, event); err != nil {
			t.Fatal(err)
		}
		message, err := subscription.NextMsgWithContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if message.Header.Get("WPP-Event-ID") != event.ID || !bytes.Equal(message.Data, event.Data) {
			t.Fatal("NATS payload/identity changed")
		}
		target.Close()
	}
}
func TestIntegrationRabbitMQConfirmedPersistentPublishAndReconnect(t *testing.T) {
	address := integrationEnv(t, "INTEGRATION_AMQP_URL")
	connection, err := amqp.Dial(address)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	name := "wppgo.integration." + uuid.NewString()
	target := &RabbitMQ{URL: address, Exchange: name, Queue: name}
	defer target.Close()
	defer func() { channel.QueueDelete(name, false, false, false); channel.ExchangeDelete(name, false, false) }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	event := Event{ID: uuid.NewString(), Session: "alpha", Name: "onmessage", Created: time.Now().UnixMilli(), Data: json.RawMessage(`{"event":"onmessage","session":"alpha","body":"integration"}`)}
	for i := 0; i < 2; i++ {
		if err = target.Deliver(ctx, event); err != nil {
			t.Fatal(err)
		}
		message, ok, err := channel.Get(name, false)
		if err != nil || !ok {
			t.Fatalf("confirmed message not available: %v %v", ok, err)
		}
		if message.MessageId != event.ID || message.DeliveryMode != amqp.Persistent || !bytes.Equal(message.Body, event.Data) {
			t.Fatal("RabbitMQ persistence/payload/identity changed")
		}
		if err = message.Ack(false); err != nil {
			t.Fatal(err)
		}
		target.Close()
	}
}
