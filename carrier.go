// Package sns provides shared types for the vinculum SNS integration.
package sns

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// MessageAttributeCarrier implements propagation.TextMapCarrier backed
// by SNS message attributes. Used by the sender (inject) for W3C trace
// context propagation. SNS forwards message attributes to SQS subscribers,
// so trace context carries through end-to-end.
type MessageAttributeCarrier struct {
	Attrs map[string]snstypes.MessageAttributeValue
}

func (c *MessageAttributeCarrier) Get(key string) string {
	if c.Attrs == nil {
		return ""
	}
	if v, ok := c.Attrs[key]; ok && v.StringValue != nil {
		return *v.StringValue
	}
	return ""
}

func (c *MessageAttributeCarrier) Set(key, value string) {
	if c.Attrs == nil {
		c.Attrs = make(map[string]snstypes.MessageAttributeValue)
	}
	c.Attrs[key] = snstypes.MessageAttributeValue{
		DataType:    aws.String("String"),
		StringValue: aws.String(value),
	}
}

func (c *MessageAttributeCarrier) Keys() []string {
	keys := make([]string, 0, len(c.Attrs))
	for k := range c.Attrs {
		keys = append(keys, k)
	}
	return keys
}
