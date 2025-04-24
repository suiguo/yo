package kafkasarama

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/IBM/sarama"
	"gopkg.in/yaml.v3"
)

const (
	InitialOffsetLatest   = "latest"
	InitialOffsetEarliest = "earliest"
)

type SimpleComsumerCfg struct {
	Brokers        []string `json:"brokers" yaml:"brokers"`                     // Kafka 地址
	ClientID       string   `json:"client_id" yaml:"client_id"`                 // 客户端 ID
	Version        string   `json:"version" yaml:"version"`                     // Kafka 版本（如 "2.1.0"）
	GroupId        string   `json:"group_id" yaml:"group_id"`                   // acks（如 "all", "local", "none"）
	FetchWaitMaxMS int      `json:"fetch_wait_max_ms" yaml:"fetch_wait_max_ms"` // 消息超时（毫秒）
	TLS            *TLSCfg  `json:"tls,omitempty" yaml:"tls,omitempty"`         // TLS 配置
	SASL           *SASLCfg `json:"sasl,omitempty" yaml:"sasl,omitempty"`       // SASL 配置
	AutoAck        bool     `json:"auto_ack,omitempty" yaml:"auto_ack,omitempty"`
	InitialOffset  string   `json:"initial_offset,omitempty" yaml:"initial_offset,omitempty"`
}

func (c *SimpleComsumerCfg) BuildOptions() (*sarama.Config, error) {
	config := sarama.NewConfig()
	if c.ClientID != "" {
		config.ClientID = c.ClientID
	}
	if c.Version != "" {
		v, err := sarama.ParseKafkaVersion(c.Version)
		if err != nil {
			return nil, err
		}
		config.Version = v
	}
	if c.FetchWaitMaxMS > 0 {
		config.Consumer.MaxWaitTime = time.Duration(c.FetchWaitMaxMS) * time.Millisecond
	}
	config.Consumer.Offsets.AutoCommit.Enable = c.AutoAck
	config.Consumer.Offsets.AutoCommit.Interval = time.Second * 5
	if c.TLS != nil {
		tlsCfg, err := createTLSConfig(c.TLS.CertFile, c.TLS.KeyFile, c.TLS.CACertFile, c.TLS.SkipVerify)
		if err != nil {
			return nil, fmt.Errorf("invalid TLS config: %v", err) // 也可以返回 error，但这层为 Option，统一 panic 更一致
		}
		config.Net.TLS.Enable = true
		config.Net.TLS.Config = tlsCfg
	}
	switch strings.ToLower(c.InitialOffset) {
	case InitialOffsetLatest:
		config.Consumer.Offsets.Initial = sarama.OffsetNewest
	case InitialOffsetEarliest:
		config.Consumer.Offsets.Initial = sarama.OffsetOldest
	}
	if c.SASL != nil {
		config.Net.SASL.Enable = true
		config.Net.SASL.User = c.SASL.User
		config.Net.SASL.Password = c.SASL.Password
		switch c.SASL.Algo {
		case SASL_SHA256:
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xDGSCRAMClient{HashGeneratorFcn: sHA256}
			}
		case SASL_SHA512:
			config.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xDGSCRAMClient{HashGeneratorFcn: sHA512}
			}
		default:
			return nil, fmt.Errorf("unsupported SASL algorithm: %s", c.SASL.Algo)
		}
	}
	return config, nil
}

func loadConfigFromYAML(file string) (*SimpleComsumerCfg, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var cfg SimpleComsumerCfg
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func loadConfigFromJSON(file string) (*SimpleComsumerCfg, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var cfg SimpleComsumerCfg
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func NewComsumerSimpleCfgFromFile(path string) (*SimpleComsumerCfg, error) {
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		return loadConfigFromYAML(path)
	} else if strings.HasSuffix(path, ".json") {
		return loadConfigFromJSON(path)
	} else {
		return nil, fmt.Errorf("unsupported file format")
	}
}

func NewComsumerSimpleCfgFromBytes(data []byte) (*SimpleComsumerCfg, error) {
	var cfg SimpleComsumerCfg
	// 尝试 YAML
	if err := yaml.Unmarshal(data, &cfg); err == nil {
		return &cfg, nil
	}
	// 尝试 JSON
	if err := json.Unmarshal(data, &cfg); err == nil {
		return &cfg, nil
	}
	return nil, fmt.Errorf("data is not valid JSON or YAML")
}
