package kafkasarama

import (
	"time"

	"github.com/IBM/sarama"
)

type SimpleProducerAsyncCfg struct {
	Brokers     []string            `json:"brokers" yaml:"brokers"`         // Kafka 地址
	ClientID    string              `json:"client_id" yaml:"client_id"`     // 客户端 ID
	Version     string              `json:"version" yaml:"version"`         // Kafka 版本（如 "2.1.0"）
	Acks        string              `json:"acks" yaml:"acks"`               // acks（如 "all", "local", "none"）
	RetryMax    int                 `json:"retry_max" yaml:"retry_max"`     // 重试次数
	Partitioner PartitionerStrategy `json:"partitioner" yaml:"partitioner"` // 分区策略
	TimeoutMS   int                 `json:"timeout_ms" yaml:"timeout_ms"`   // 消息超时（毫秒）

	TLS  *TLSCfg  `json:"tls,omitempty" yaml:"tls,omitempty"`   // TLS 配置
	SASL *SASLCfg `json:"sasl,omitempty" yaml:"sasl,omitempty"` // SASL 配置
}

func (c *SimpleProducerAsyncCfg) BuildOptions() *sarama.Config {
	config := sarama.NewConfig()
	opts := make([]ProducerAsyncOption, 0)
	config.Producer.Return.Successes = true
	if c.ClientID != "" {
		opts = append(opts, WithClientID(c.ClientID))
	}
	if c.Version != "" {
		if v, err := sarama.ParseKafkaVersion(c.Version); err == nil {
			opts = append(opts, WithVersion(v))
		}
	}
	switch c.Acks {
	case "all":
		opts = append(opts, WithRequiredAcks(sarama.WaitForAll))
	case "local":
		opts = append(opts, WithRequiredAcks(sarama.WaitForLocal))
	case "none":
		opts = append(opts, WithRequiredAcks(sarama.NoResponse))
	}
	if c.RetryMax > 0 {
		opts = append(opts, WithRetryMax(c.RetryMax))
	}
	if c.Partitioner != "" {
		opts = append(opts, WithPartitioner(c.Partitioner))
	}
	if c.TimeoutMS > 0 {
		opts = append(opts, WithMessageTimeout(time.Duration(c.TimeoutMS)*time.Millisecond))
	}
	if c.Acks == "" {
		opts = append(opts, WithRequiredAcks(sarama.WaitForAll))
	}
	if c.TLS != nil {
		opts = append(opts, WithTLS(
			c.TLS.CertFile,
			c.TLS.KeyFile,
			c.TLS.CACertFile,
			c.TLS.SkipVerify,
		))
	}
	if c.SASL != nil {
		opts = append(opts, WithSASL(c.SASL.User, c.SASL.Password, c.SASL.Algo))
	}
	for _, o := range opts {
		o(config)
	}
	return config
}

type TLSCfg struct {
	CertFile   string `json:"cert_file" yaml:"cert_file"`
	KeyFile    string `json:"key_file" yaml:"key_file"`
	CACertFile string `json:"ca_cert_file" yaml:"ca_cert_file"`
	SkipVerify bool   `json:"skip_verify" yaml:"skip_verify"`
}

type SASLCfg struct {
	User     string        `json:"user" yaml:"user"`
	Password string        `json:"password" yaml:"password"`
	Algo     SASLAlgorithm `json:"algo" yaml:"algo"`
}
