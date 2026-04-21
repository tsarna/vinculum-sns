# vinculum-sns

Amazon SNS client package for [Vinculum](https://github.com/tsarna/vinculum), built on the [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2).

Provides an `SNSSender` (sink) that integrates with the `vinculum-bus` event bus. The VCL configuration wiring lives in the main vinculum repo (`clients/sns/`) to avoid circular imports -- this module only depends on `vinculum-bus`.

---

## Packages

### `sender` -- SNSSender

Implements `bus.Subscriber`. `OnEvent` serializes the payload, maps vinculum fields to SNS message attributes, and publishes the message to a configured SNS target.

**Features:**
- Wire-format serialization via `vinculum-wire` (`auto`, `json`, `string`, `bytes`)
- Vinculum fields mapped to SNS `MessageAttribute`s
- `$CamelCase` fields (`$Subject`, `$MessageStructure`) mapped to SNS publish properties
- Optional `topic_attribute` to include the vinculum topic as a message attribute
- SNS attribute name validation and 10-attribute budget enforcement
- Auto-detection of SNS target type from value: TopicArn, TargetArn, or PhoneNumber
- FIFO topic support: per-message `MessageGroupId` and `MessageDeduplicationId` via configurable functions
- W3C trace context propagation (inject into message attributes)
- OTel metrics instrumentation (sent count, operation duration)

**Builder example:**

```go
sender, err := sender.NewSender().
    WithClient(snsClient).
    WithClientName("alerts").
    WithStaticTarget("arn:aws:sns:us-east-1:123456789012:alerts").
    WithWireFormat(wire.JSON).
    WithTopicAttribute("source_topic").
    WithMeterProvider(mp).
    WithLogger(logger).
    Build()
```

### Root package -- shared types

`MessageAttributeCarrier` implements `propagation.TextMapCarrier` backed by SNS message attributes, used by the sender (inject) for W3C trace context propagation. SNS forwards message attributes to SQS subscribers, so trace context carries through the SNS -> SQS path.

---

## Metrics

The sender exposes instrumentation via an OTel `metric.MeterProvider`. Pass `nil` to disable metrics (all methods are nil-safe).

| Metric | Type | Unit | Description |
| ------ | ---- | ---- | ----------- |
| `messaging.client.sent.messages` | Int64Counter | `{message}` | Messages published |
| `messaging.client.operation.duration` | Float64Histogram | `s` | Publish latency |

All metrics and trace spans carry attributes: `messaging.system=aws_sns`, `messaging.destination.name=<topic>`, `vinculum.client.name=<client>`.

---

## VCL configuration

When used via vinculum, the `sns_sender` client type is available:

```hcl
# Shared AWS credentials
client "aws" "prod" {
    region = "us-east-1"
}

# Send vinculum bus events to an SNS topic
client "sns_sender" "alerts" {
    aws       = client.prod
    sns_topic = "arn:aws:sns:us-east-1:123456789012:alerts"
}

subscription "to_sns" {
    target     = bus.main
    topics     = ["alert/#"]
    subscriber = client.alerts
}
```

---

## Dependencies

- [`github.com/aws/aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2) -- AWS SDK for Go v2 (SNS client)
- [`github.com/tsarna/vinculum-bus`](https://github.com/tsarna/vinculum-bus) -- `Subscriber`, `EventBus` interfaces
- [`github.com/tsarna/vinculum-wire`](https://github.com/tsarna/vinculum-wire) -- `WireFormat` interface and built-in formats
- [`go.uber.org/zap`](https://pkg.go.dev/go.uber.org/zap) -- structured logging
- [`go.opentelemetry.io/otel`](https://pkg.go.dev/go.opentelemetry.io/otel) -- tracing and metrics

## License

BSD 2-Clause -- see [LICENSE](LICENSE).
