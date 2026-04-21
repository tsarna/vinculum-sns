package sender

import (
	"errors"

	wire "github.com/tsarna/vinculum-wire"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// SenderBuilder constructs an SNSSender with validated configuration.
type SenderBuilder struct {
	client         SNSPublishAPI
	clientName     string
	staticTarget   string
	wireFormat     wire.WireFormat
	topicAttribute string
	meterProvider  metric.MeterProvider
	logger         *zap.Logger
	tracerProvider trace.TracerProvider
}

// NewSender returns a builder with sensible defaults.
func NewSender() *SenderBuilder {
	return &SenderBuilder{
		logger: zap.NewNop(),
	}
}

func (b *SenderBuilder) WithClient(c SNSPublishAPI) *SenderBuilder {
	b.client = c
	return b
}

func (b *SenderBuilder) WithClientName(name string) *SenderBuilder {
	b.clientName = name
	return b
}

func (b *SenderBuilder) WithStaticTarget(target string) *SenderBuilder {
	b.staticTarget = target
	return b
}

func (b *SenderBuilder) WithWireFormat(wf wire.WireFormat) *SenderBuilder {
	b.wireFormat = wf
	return b
}

func (b *SenderBuilder) WithTopicAttribute(name string) *SenderBuilder {
	b.topicAttribute = name
	return b
}

func (b *SenderBuilder) WithLogger(l *zap.Logger) *SenderBuilder {
	if l != nil {
		b.logger = l
	}
	return b
}

func (b *SenderBuilder) WithTracerProvider(tp trace.TracerProvider) *SenderBuilder {
	b.tracerProvider = tp
	return b
}

func (b *SenderBuilder) WithMeterProvider(mp metric.MeterProvider) *SenderBuilder {
	b.meterProvider = mp
	return b
}

// Build validates the configuration and returns a ready-to-use SNSSender.
func (b *SenderBuilder) Build() (*SNSSender, error) {
	if b.client == nil {
		return nil, errors.New("sns sender: client is required")
	}
	if b.staticTarget == "" {
		return nil, errors.New("sns sender: target is required")
	}

	// Resolve and validate the static target.
	property, name, err := ResolveTarget(b.staticTarget)
	if err != nil {
		return nil, err
	}

	wf := b.wireFormat
	if wf == nil {
		wf = wire.Auto
	}

	return &SNSSender{
		client:         b.client,
		clientName:     b.clientName,
		wireFormat:     wf,
		staticTarget:   b.staticTarget,
		topicProperty:  property,
		topicName:      name,
		topicAttribute: b.topicAttribute,
		metrics:        NewSenderMetrics(b.clientName, name, b.meterProvider),
		logger:         b.logger,
		tracerProvider: b.tracerProvider,
	}, nil
}
