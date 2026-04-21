package sender

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// SenderMetrics instruments the SNS sender with OTel metrics.
// A nil *SenderMetrics is safe to use — all methods are no-ops.
type SenderMetrics struct {
	messagesSent      metric.Int64Counter
	operationDuration metric.Float64Histogram
	batchMessageCount metric.Float64Histogram
	baseAttrs         metric.MeasurementOption
}

// NewSenderMetrics creates sender metrics from a MeterProvider. Returns
// a noop-backed instance if mp is nil. clientName is the vinculum client
// block name.
func NewSenderMetrics(clientName string, mp metric.MeterProvider) *SenderMetrics {
	if mp == nil {
		mp = noop.NewMeterProvider()
	}
	meter := mp.Meter("github.com/tsarna/vinculum-sns/sender")
	sent, _ := meter.Int64Counter("messaging.client.sent.messages",
		metric.WithUnit("{message}"),
		metric.WithDescription("Messages published by the SNS sender"),
	)
	duration, _ := meter.Float64Histogram("messaging.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of SNS publish operations"),
	)
	batchSize, _ := meter.Float64Histogram("messaging.batch.message_count",
		metric.WithUnit("{message}"),
		metric.WithDescription("Messages per SNS batch publish"),
	)
	return &SenderMetrics{
		messagesSent:      sent,
		operationDuration: duration,
		batchMessageCount: batchSize,
		baseAttrs: metric.WithAttributes(
			attribute.String("messaging.system", "aws_sns"),
			attribute.String("vinculum.client.name", clientName),
		),
	}
}

func topicAttr(topic string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String("messaging.destination.name", topic))
}

func (m *SenderMetrics) RecordSent(ctx context.Context, destName string) {
	if m == nil {
		return
	}
	m.messagesSent.Add(ctx, 1, m.baseAttrs, topicAttr(destName))
}

func (m *SenderMetrics) RecordOperationDuration(ctx context.Context, d time.Duration, destName string) {
	if m == nil {
		return
	}
	m.operationDuration.Record(ctx, d.Seconds(), m.baseAttrs, topicAttr(destName))
}

func (m *SenderMetrics) RecordBatchSize(ctx context.Context, size int, destName string) {
	if m == nil {
		return
	}
	m.batchMessageCount.Record(ctx, float64(size), m.baseAttrs, topicAttr(destName))
}
