package sender

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	wire "github.com/tsarna/vinculum-wire"
)

// mockSNS captures Publish calls for testing.
type mockSNS struct {
	publishInput *sns.PublishInput
	publishErr   error
	messageID    string
}

func (m *mockSNS) Publish(_ context.Context, params *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	m.publishInput = params
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	return &sns.PublishOutput{MessageId: &m.messageID}, nil
}

func buildTestSender(t *testing.T, mock *mockSNS, opts ...func(*SenderBuilder)) *SNSSender {
	t.Helper()
	b := NewSender().
		WithClient(mock).
		WithClientName("test").
		WithStaticTarget("arn:aws:sns:us-east-1:123456789012:alerts")
	for _, opt := range opts {
		opt(b)
	}
	s, err := b.Build()
	require.NoError(t, err)
	return s
}

func TestOnEvent_BasicSend(t *testing.T) {
	mock := &mockSNS{messageID: "msg-123"}
	s := buildTestSender(t, mock)

	err := s.OnEvent(context.Background(), "alert/fire", "hello world", nil)
	require.NoError(t, err)

	assert.NotNil(t, mock.publishInput)
	assert.Equal(t, "hello world", *mock.publishInput.Message)
	assert.Equal(t, "arn:aws:sns:us-east-1:123456789012:alerts", *mock.publishInput.TopicArn)
	assert.Nil(t, mock.publishInput.TargetArn)
	assert.Nil(t, mock.publishInput.PhoneNumber)
}

func TestOnEvent_StringPayload(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	s := buildTestSender(t, mock)

	err := s.OnEvent(context.Background(), "test", "plain string", nil)
	require.NoError(t, err)
	assert.Equal(t, "plain string", *mock.publishInput.Message)
}

func TestOnEvent_FieldsToAttributes(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	s := buildTestSender(t, mock)

	fields := map[string]string{
		"severity": "high",
		"source":   "monitor",
	}
	err := s.OnEvent(context.Background(), "alert/cpu", "cpu high", fields)
	require.NoError(t, err)

	attrs := mock.publishInput.MessageAttributes
	assert.Contains(t, attrs, "severity")
	assert.Equal(t, "high", *attrs["severity"].StringValue)
	assert.Contains(t, attrs, "source")
	assert.Equal(t, "monitor", *attrs["source"].StringValue)
}

func TestOnEvent_DollarFieldsToProperties(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	s := buildTestSender(t, mock)

	fields := map[string]string{
		"$Subject":          "Alert: CPU",
		"$MessageStructure": "json",
		"severity":          "high",
	}
	err := s.OnEvent(context.Background(), "alert/cpu", `{"default":"cpu high"}`, fields)
	require.NoError(t, err)

	// $Subject → PublishInput.Subject
	assert.Equal(t, "Alert: CPU", *mock.publishInput.Subject)
	// $MessageStructure → PublishInput.MessageStructure
	assert.Equal(t, "json", *mock.publishInput.MessageStructure)

	// $-prefixed fields should NOT appear in message attributes.
	attrs := mock.publishInput.MessageAttributes
	assert.NotContains(t, attrs, "$Subject")
	assert.NotContains(t, attrs, "$MessageStructure")
	// Non-$ fields should still be present.
	assert.Contains(t, attrs, "severity")
}

func TestOnEvent_TopicAttribute(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	s := buildTestSender(t, mock, func(b *SenderBuilder) {
		b.WithTopicAttribute("source_topic")
	})

	err := s.OnEvent(context.Background(), "order/created", "new order", nil)
	require.NoError(t, err)

	attrs := mock.publishInput.MessageAttributes
	assert.Contains(t, attrs, "source_topic")
	assert.Equal(t, "order/created", *attrs["source_topic"].StringValue)
}

func TestOnEvent_SendError(t *testing.T) {
	mock := &mockSNS{publishErr: errors.New("access denied")}
	s := buildTestSender(t, mock)

	err := s.OnEvent(context.Background(), "test", "msg", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
	assert.Contains(t, err.Error(), "sns sender")
}

func TestOnEvent_SerializeError(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	b := NewSender().
		WithClient(mock).
		WithClientName("test").
		WithStaticTarget("arn:aws:sns:us-east-1:123456789012:alerts").
		WithWireFormat(&failingWireFormat{})
	s, err := b.Build()
	require.NoError(t, err)

	err = s.OnEvent(context.Background(), "test", "msg", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "serialize")
}

type failingWireFormat struct{}

func (f *failingWireFormat) Serialize(msg any) ([]byte, error) {
	return nil, errors.New("serialize failed")
}

func (f *failingWireFormat) SerializeString(msg any) (string, error) {
	return "", errors.New("serialize failed")
}

func (f *failingWireFormat) Deserialize(data []byte) (any, error) {
	return nil, errors.New("not implemented")
}

func (f *failingWireFormat) Name() string {
	return "failing"
}

func TestResolveTarget(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		property string
		topicN   string
		wantErr  bool
	}{
		{
			name:     "topic ARN",
			value:    "arn:aws:sns:us-east-1:123456789012:alerts",
			property: PropertyTopicArn,
			topicN:   "alerts",
		},
		{
			name:     "FIFO topic ARN",
			value:    "arn:aws:sns:us-east-1:123456789012:orders.fifo",
			property: PropertyTopicArn,
			topicN:   "orders.fifo",
		},
		{
			name:     "target ARN (endpoint)",
			value:    "arn:aws:sns:us-east-1:123456789012:endpoint/GCM/myapp/abc123",
			property: PropertyTargetArn,
			topicN:   "abc123",
		},
		{
			name:     "phone number",
			value:    "+14155552671",
			property: PropertyPhoneNumber,
			topicN:   "+14155552671",
		},
		{
			name:    "empty string",
			value:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			value:   "not-an-arn",
			wantErr: true,
		},
		{
			name:    "non-SNS ARN",
			value:   "arn:aws:sqs:us-east-1:123456789012:my-queue",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prop, name, err := ResolveTarget(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.property, prop)
			assert.Equal(t, tt.topicN, name)
		})
	}
}

func TestIsFIFOTopic(t *testing.T) {
	assert.True(t, IsFIFOTopic("arn:aws:sns:us-east-1:123456789012:orders.fifo"))
	assert.False(t, IsFIFOTopic("arn:aws:sns:us-east-1:123456789012:orders"))
}

func TestIsValidAttributeName(t *testing.T) {
	assert.True(t, isValidAttributeName("valid-name"))
	assert.True(t, isValidAttributeName("valid_name"))
	assert.True(t, isValidAttributeName("valid.name"))
	assert.True(t, isValidAttributeName("ValidName123"))
	assert.False(t, isValidAttributeName(""))
	assert.False(t, isValidAttributeName("has space"))
	assert.False(t, isValidAttributeName("has=equals"))
	assert.False(t, isValidAttributeName("AWS.reserved"))
	assert.False(t, isValidAttributeName("aws.reserved"))
	assert.False(t, isValidAttributeName("Amazon.reserved"))
	assert.False(t, isValidAttributeName("amazon.reserved"))
}

func TestBuilder_Validation(t *testing.T) {
	mock := &mockSNS{}

	// Missing client.
	_, err := NewSender().
		WithStaticTarget("arn:aws:sns:us-east-1:123456789012:alerts").
		Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client is required")

	// Missing target.
	_, err = NewSender().
		WithClient(mock).
		Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target is required")

	// Invalid target.
	_, err = NewSender().
		WithClient(mock).
		WithStaticTarget("not-valid").
		Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognized format")

	// Defaults wire format to Auto.
	s, err := NewSender().
		WithClient(mock).
		WithClientName("test").
		WithStaticTarget("arn:aws:sns:us-east-1:123456789012:alerts").
		Build()
	require.NoError(t, err)
	assert.Equal(t, wire.Auto, s.wireFormat)
}

func TestOnEvent_AttributeBudget(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	s := buildTestSender(t, mock)

	// Create more fields than the budget allows (10 - 3 trace = 7 max).
	fields := map[string]string{}
	for i := 0; i < 10; i++ {
		fields[fmt.Sprintf("field%d", i)] = "value"
	}

	err := s.OnEvent(context.Background(), "test", "msg", fields)
	require.NoError(t, err)

	// Should have at most 7 user attributes (10 - 3 reserved for trace).
	userAttrs := 0
	for k := range mock.publishInput.MessageAttributes {
		if !traceAttributeKeys[k] {
			userAttrs++
		}
	}
	assert.LessOrEqual(t, userAttrs, 7)
}

func TestOnEvent_BaseSubscriberNoOps(t *testing.T) {
	mock := &mockSNS{messageID: "msg-1"}
	s := buildTestSender(t, mock)

	// These should be no-ops from BaseSubscriber.
	assert.NoError(t, s.OnSubscribe(context.Background(), "test"))
	assert.NoError(t, s.OnUnsubscribe(context.Background(), "test"))
}
