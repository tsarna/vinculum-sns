// Package sender provides SNSSender, which implements bus.Subscriber to
// forward vinculum bus events to AWS SNS via Publish.
package sender

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	bus "github.com/tsarna/vinculum-bus"
	vsns "github.com/tsarna/vinculum-sns"
	wire "github.com/tsarna/vinculum-wire"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// maxSNSAttributes is the SNS limit on message attributes per message.
const maxSNSAttributes = 10

// traceAttributeKeys are the W3C trace context keys injected by OTel
// propagators. These take priority over user fields in the attribute budget.
var traceAttributeKeys = map[string]bool{
	"traceparent": true,
	"tracestate":  true,
	"baggage":     true,
}

// SNS target property types.
const (
	PropertyTopicArn   = "TopicArn"
	PropertyTargetArn  = "TargetArn"
	PropertyPhoneNumber = "PhoneNumber"
)

// SNSPublishAPI is the subset of the SNS client API used for single publishes.
type SNSPublishAPI interface {
	Publish(ctx context.Context, params *sns.PublishInput, optFns ...func(*sns.Options)) (*sns.PublishOutput, error)
}

// SNSSender receives vinculum bus events and publishes them to AWS SNS.
// It implements bus.Subscriber so it can be used directly as a subscription target.
type SNSSender struct {
	bus.BaseSubscriber

	client         SNSPublishAPI
	clientName     string // vinculum client block name
	wireFormat     wire.WireFormat
	staticTarget   string // resolved target value (when constant)
	topicProperty  string // "TopicArn", "TargetArn", or "PhoneNumber" (when static)
	topicName      string // human-readable name extracted from ARN (when static)
	topicAttribute string // "" = don't include
	metrics        *SenderMetrics
	logger         *zap.Logger
	tracerProvider trace.TracerProvider
}

func (s *SNSSender) tracer() trace.Tracer {
	tp := s.tracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	return tp.Tracer("github.com/tsarna/vinculum-sns/sender")
}

// Start starts the batcher if batching is enabled. No-op for now.
func (s *SNSSender) Start() {}

// Stop stops the batcher if batching is enabled. No-op for now.
func (s *SNSSender) Stop() {}

// OnEvent serializes the message, maps fields to SNS message attributes,
// and publishes the message to the configured SNS target.
func (s *SNSSender) OnEvent(ctx context.Context, topic string, msg any, fields map[string]string) error {
	start := time.Now()

	// Serialize payload.
	body, err := s.wireFormat.SerializeString(msg)
	if err != nil {
		return fmt.Errorf("sns sender %s: serialize: %w", s.topicName, err)
	}

	// Extract $CamelCase fields as SNS properties.
	subject := extractField(fields, "$Subject")
	msgStructure := extractField(fields, "$MessageStructure")

	// Build message attributes from non-$ fields.
	attrs := s.buildMessageAttributes(fields, topic)

	// Inject trace context into message attributes.
	propagator := otel.GetTextMapPropagator()
	carrier := &vsns.MessageAttributeCarrier{Attrs: attrs}
	propagator.Inject(ctx, carrier)

	// Start tracing span.
	ctx, span := s.tracer().Start(ctx, "publish "+s.topicName,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "aws_sns"),
			attribute.String("messaging.destination.name", s.topicName),
			attribute.String("messaging.operation.type", "publish"),
			attribute.String("vinculum.client.name", s.clientName),
		),
	)
	defer span.End()

	// Build publish input.
	input := &sns.PublishInput{
		Message:           &body,
		MessageAttributes: attrs,
	}

	// Set target property.
	switch s.topicProperty {
	case PropertyTopicArn:
		input.TopicArn = &s.staticTarget
	case PropertyTargetArn:
		input.TargetArn = &s.staticTarget
	case PropertyPhoneNumber:
		input.PhoneNumber = &s.staticTarget
	}

	// Set optional properties.
	if subject != "" {
		input.Subject = &subject
	}
	if msgStructure != "" {
		input.MessageStructure = &msgStructure
	}

	// Publish.
	result, err := s.client.Publish(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("sns sender %s: publish: %w", s.topicName, err)
	}

	if result.MessageId != nil {
		span.SetAttributes(attribute.String("messaging.message.id", *result.MessageId))
	}

	s.metrics.RecordSent(ctx)
	s.metrics.RecordOperationDuration(ctx, time.Since(start))
	return nil
}

// buildMessageAttributes converts vinculum fields to SNS message attributes.
// $CamelCase fields are excluded (consumed as SNS properties by the caller).
// All other fields become message attributes with DataType "String".
func (s *SNSSender) buildMessageAttributes(fields map[string]string, topic string) map[string]snstypes.MessageAttributeValue {
	attrs := make(map[string]snstypes.MessageAttributeValue, len(fields)+1)

	// Reserve slots for trace attributes (injected after this function).
	budget := maxSNSAttributes - 3

	// Add topic attribute if configured.
	if s.topicAttribute != "" {
		attrs[s.topicAttribute] = snstypes.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(topic),
		}
		budget--
	}

	// Map vinculum fields to SNS message attributes.
	for k, v := range fields {
		if budget <= 0 {
			s.logger.Warn("sns sender: dropping excess fields, attribute limit reached",
				zap.String("topic", s.topicName),
				zap.Int("limit", maxSNSAttributes),
			)
			break
		}

		// Skip $CamelCase fields — they are consumed as SNS properties.
		if strings.HasPrefix(k, "$") {
			continue
		}

		if !isValidAttributeName(k) {
			s.logger.Debug("sns sender: dropping field with invalid SNS attribute name",
				zap.String("field", k),
			)
			continue
		}

		attrs[k] = snstypes.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(v),
		}
		budget--
	}

	return attrs
}

// extractField returns the value of a field from the fields map and returns "".
// It does NOT delete from the map — the caller skips $ fields during attribute building.
func extractField(fields map[string]string, key string) string {
	if fields == nil {
		return ""
	}
	return fields[key]
}

// ResolveTarget determines the SNS Publish property from a target value.
// Returns the property name ("TopicArn", "TargetArn", "PhoneNumber") and
// a human-readable name for metrics/tracing.
func ResolveTarget(value string) (property, name string, err error) {
	if value == "" {
		return "", "", errors.New("sns target value is empty")
	}

	// Phone number: starts with "+"
	if strings.HasPrefix(value, "+") {
		return PropertyPhoneNumber, value, nil
	}

	// ARN: starts with "arn:aws:sns:"
	if strings.HasPrefix(value, "arn:aws:sns:") {
		// Split ARN: arn:aws:sns:REGION:ACCOUNT:RESOURCE
		parts := strings.SplitN(value, ":", 7)
		if len(parts) < 6 {
			return "", "", fmt.Errorf("sns target: invalid ARN format: %s", value)
		}
		resource := parts[5]
		if len(parts) == 7 {
			resource = parts[5] + ":" + parts[6]
		}

		// Resource contains "/" → TargetArn (endpoint ARN)
		if strings.Contains(resource, "/") {
			// Extract a short name from endpoint ARNs for display.
			name = resource
			if idx := strings.LastIndex(resource, "/"); idx >= 0 && idx < len(resource)-1 {
				name = resource[idx+1:]
			}
			return PropertyTargetArn, name, nil
		}

		// Resource is a bare name → TopicArn
		return PropertyTopicArn, resource, nil
	}

	return "", "", fmt.Errorf("sns target: unrecognized format (expected ARN or phone number): %s", value)
}

// IsFIFOTopic returns true if the topic ARN ends with ".fifo".
func IsFIFOTopic(arn string) bool {
	return strings.HasSuffix(arn, ".fifo")
}

// isValidAttributeName checks whether a name is valid for an SNS message
// attribute. Valid names contain only alphanumeric characters, hyphens,
// underscores, and periods, and must not start with "AWS." or "Amazon."
// (case-insensitive).
func isValidAttributeName(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "aws.") || strings.HasPrefix(lower, "amazon.") {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return false
		}
	}
	return true
}
