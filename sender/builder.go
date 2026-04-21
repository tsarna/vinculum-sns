package sender

import (
	"errors"

	wire "github.com/tsarna/vinculum-wire"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// FIFOConfig holds the per-message hooks for FIFO topic parameters.
type FIFOConfig struct {
	GroupIDHook       HookFunc // required for FIFO topics
	DeduplicationHook HookFunc // nil = use topic's content-based dedup
}

// SenderBuilder constructs an SNSSender with validated configuration.
type SenderBuilder struct {
	client         SNSPublishAPI
	clientName     string
	staticTarget   string
	topicHook      HookFunc
	passthrough    bool
	subjectHook    HookFunc
	msgStructure   string
	wireFormat     wire.WireFormat
	topicAttribute string
	fifo           *FIFOConfig
	makeHookCtx    MakeHookContextFunc
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

func (b *SenderBuilder) WithTopicHook(fn HookFunc) *SenderBuilder {
	b.topicHook = fn
	return b
}

func (b *SenderBuilder) WithPassthrough() *SenderBuilder {
	b.passthrough = true
	return b
}

func (b *SenderBuilder) WithSubjectHook(fn HookFunc) *SenderBuilder {
	b.subjectHook = fn
	return b
}

func (b *SenderBuilder) WithMakeHookContext(fn MakeHookContextFunc) *SenderBuilder {
	b.makeHookCtx = fn
	return b
}

func (b *SenderBuilder) WithMessageStructure(ms string) *SenderBuilder {
	b.msgStructure = ms
	return b
}

func (b *SenderBuilder) WithFIFOConfig(cfg *FIFOConfig) *SenderBuilder {
	b.fifo = cfg
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

	// Validate exactly one target mode is configured.
	modes := 0
	if b.staticTarget != "" {
		modes++
	}
	if b.topicHook != nil {
		modes++
	}
	if b.passthrough {
		modes++
	}
	if modes == 0 {
		return nil, errors.New("sns sender: target is required (use WithStaticTarget, WithTopicHook, or WithPassthrough)")
	}
	if modes > 1 {
		return nil, errors.New("sns sender: only one target mode allowed (static, topic hook, or passthrough)")
	}

	var (
		staticTarget  string
		topicProperty string
		topicName     string
	)

	if b.staticTarget != "" {
		// Resolve and validate the static target.
		var err error
		topicProperty, topicName, err = ResolveTarget(b.staticTarget)
		if err != nil {
			return nil, err
		}
		staticTarget = b.staticTarget

		// FIFO validation: .fifo topics require message_group_id.
		if IsFIFOTopic(staticTarget) && (b.fifo == nil || b.fifo.GroupIDHook == nil) {
			return nil, errors.New("sns sender: FIFO topic requires message_group_id (topic ARN ends in .fifo)")
		}
	}

	wf := b.wireFormat
	if wf == nil {
		wf = wire.Auto
	}

	makeHookCtx := b.makeHookCtx
	if makeHookCtx == nil {
		makeHookCtx = func(string, any, map[string]string) (HookContext, error) {
			return nil, nil
		}
	}

	return &SNSSender{
		client:         b.client,
		clientName:     b.clientName,
		wireFormat:     wf,
		staticTarget:   staticTarget,
		topicProperty:  topicProperty,
		topicName:      topicName,
		topicHook:      b.topicHook,
		passthrough:    b.passthrough,
		subjectHook:    b.subjectHook,
		msgStructure:   b.msgStructure,
		topicAttribute: b.topicAttribute,
		fifo:           b.fifo,
		makeHookCtx:    makeHookCtx,
		metrics:        NewSenderMetrics(b.clientName, b.meterProvider),
		logger:         b.logger,
		tracerProvider: b.tracerProvider,
	}, nil
}
