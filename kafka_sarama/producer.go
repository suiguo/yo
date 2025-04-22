package kafkasarama

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/bytedance/sonic"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type PartitionerStrategy string

const (
	PartitionerRandom     PartitionerStrategy = "random"
	PartitionerHash       PartitionerStrategy = "hash"
	PartitionerRoundRobin PartitionerStrategy = "roundrobin"
	PartitionerManual     PartitionerStrategy = "manual"
)

var (
	ErrNotInitialized = errors.New("producer not initialized")
	ErrTimeout        = errors.New("send message timeout")
	ErrNotWork        = errors.New("producer is not available")
	ErrNilMessage     = errors.New("json message is nil")
	ErrTopicEmpty     = errors.New("topic is empty")
)

type SASLAlgorithm string

const (
	SASL_SHA256 SASLAlgorithm = "sha256"
	SASL_SHA512 SASLAlgorithm = "sha512"
)

// ProducerAsync 封装了异步 Kafka 消息发送接口，支持多种格式与 Key 指定发送
type ProducerAsync interface {
	PushMessage(topic string, data []byte) error
	PushJsonMessage(topic string, data any) error
	PushStringMessage(topic string, data string) error
	PushMessageWithKey(topic, key string, data []byte) error
	PushJsonMessageWithKey(topic, key string, data any) error
	PushStringMessageWithKey(topic, key, data string) error
	Close()
}

type producerAsync struct {
	base           sarama.AsyncProducer
	closeCtx       context.Context
	closeCtxCancle context.CancelFunc
	once           sync.Once
	log            *zap.Logger
	ProducerAsync
}

func (p *producerAsync) handlerError() {
	p.once.Do(func() {
		go func() {
			for {
				select {
				case <-p.closeCtx.Done():
					return
				case err, ok := <-p.base.Errors():
					if !ok {
						if p.log != nil {
							p.log.Info("producer error channel closed")
						}
						p.closeCtxCancle()
						return
					}
					if p.log != nil {
						p.log.Error("producer error", zap.Error(err))
					}
				}
			}
		}()
		go func() {
			for {
				select {
				case <-p.closeCtx.Done():
					return
				case success, ok := <-p.base.Successes():
					if !ok {
						if p.log != nil {
							p.log.Info("producer success channel closed")
						}
						p.closeCtxCancle()
						return
					}
					if p.log != nil {
						p.log.Error("producer success", zap.String("topic", success.Topic), zap.Int64("offset", success.Offset))
					}
				}
			}
		}()
	})
}

func (p *producerAsync) PushMessage(topic string, data []byte) error {
	if data == nil {
		return ErrNilMessage
	}
	if p.base == nil {
		return ErrNotInitialized
	}
	if topic == "" {
		return ErrTopicEmpty
	}
	message := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(data),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	select {
	case <-ctx.Done():
		return ErrTimeout
	case <-p.closeCtx.Done():
		return ErrNotWork
	case p.base.Input() <- message:
		if p.log != nil {
			p.log.Info("PushMessage", zap.Any("message", message))
		}
		return nil
	}
}

func (p *producerAsync) PushJsonMessage(topic string, data any) error {
	if data == nil {
		return ErrNilMessage
	}
	d, err := sonic.Marshal(data)
	if err != nil {
		return err
	}
	return p.PushMessage(topic, d)
}
func (p *producerAsync) PushStringMessage(topic string, data string) error {
	return p.PushMessage(topic, []byte(data))
}

func (p *producerAsync) PushMessageWithKey(topic, key string, data []byte) error {
	if data == nil {
		return ErrNilMessage
	}
	if topic == "" {
		return ErrTopicEmpty
	}
	if p.base == nil {
		return ErrNotInitialized
	}
	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(data),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return ErrTimeout
	case <-p.closeCtx.Done():
		return ErrNotWork
	case p.base.Input() <- msg:
		return nil
	}
}

func (p *producerAsync) PushJsonMessageWithKey(topic, key string, data any) error {
	if data == nil {
		return ErrNilMessage
	}
	b, err := sonic.Marshal(data)
	if err != nil {
		return err
	}
	return p.PushMessageWithKey(topic, key, b)
}

func (p *producerAsync) PushStringMessageWithKey(topic, key, data string) error {
	return p.PushMessageWithKey(topic, key, []byte(data))
}

func (p *producerAsync) Close() {
	p.closeCtxCancle()
	p.base.AsyncClose()
}

type ProducerAsyncOption func(*sarama.Config)

// NewProducerAsyncFromSimpleCfg 根据配置结构体构建异步生产者
func NewProducerAsyncFromSimpleCfg(cfg *SimpleProducerAsyncCfg, logger *zap.Logger) (ProducerAsync, error) {
	if cfg == nil {
		return nil, errors.New("kafka config is nil")
	}
	config := cfg.BuildOptions()
	return NewProducerAsync(cfg.Brokers, logger, config)
}

// NewProducerAsyncFromSimpleFile 根据配置文件构建异步生产者
func NewProducerAsyncFromSimpleFile(path string, logger *zap.Logger) (ProducerAsync, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file failed: %w", err)
	}
	var cfg SimpleProducerAsyncCfg
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	} else if strings.HasSuffix(path, ".json") {
		if err := sonic.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unsupported file format")
	}
	return NewProducerAsyncFromSimpleCfg(&cfg, logger)
}

func NewProducerAsync(brokers []string, logger *zap.Logger, config *sarama.Config) (ProducerAsync, error) {
	if len(brokers) == 0 {
		return nil, errors.New("brokers is empty")
	}
	if config == nil {
		return nil, errors.New("config is empty")
	}
	base, err := sarama.NewAsyncProducer(brokers, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create async producer: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &producerAsync{
		base:           base,
		log:            logger,
		closeCtx:       ctx,
		closeCtxCancle: cancel,
	}
	p.handlerError()

	if logger != nil {
		logger.Info("Kafka async producer started", zap.Strings("brokers", brokers))
	}

	return p, nil
}

func WithRetryMax(n int) ProducerAsyncOption {
	return func(c *sarama.Config) {
		c.Producer.Retry.Max = n
	}
}

func WithRequiredAcks(acks sarama.RequiredAcks) ProducerAsyncOption {
	return func(c *sarama.Config) {
		c.Producer.RequiredAcks = acks
	}
}

func WithVersion(version sarama.KafkaVersion) ProducerAsyncOption {
	return func(c *sarama.Config) {
		c.Version = version
	}
}

func createTLSConfig(certFile, keyFile, caFile string, skipVerify bool) (*tls.Config, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: skipVerify}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("invalid CA cert")
		}
		tlsCfg.RootCAs = caPool
	}

	return tlsCfg, nil
}

// WithTLS 启用带客户端证书的 TLS 连接
// certFile/keyFile/caFile 为 PEM 格式路径，skipVerify 是否跳过服务端校验
func WithTLS(certFile, keyFile, caFile string, skipVerify bool) ProducerAsyncOption {
	return func(cfg *sarama.Config) {
		tlsCfg, err := createTLSConfig(certFile, keyFile, caFile, skipVerify)
		if err != nil {
			panic(fmt.Sprintf("invalid TLS config: %v", err)) // 也可以返回 error，但这层为 Option，统一 panic 更一致
		}
		cfg.Net.TLS.Enable = true
		cfg.Net.TLS.Config = tlsCfg
	}
}

// WithTLSNoCert 启用 TLS 但不提供客户端证书，适用于 SASL + TLS 单向验证
func WithTLSNoCert(skipVerify bool) ProducerAsyncOption {
	return func(cfg *sarama.Config) {
		cfg.Net.TLS.Enable = true
		cfg.Net.TLS.Config = &tls.Config{
			InsecureSkipVerify: skipVerify,
		}
	}
}

// WithSASL 配置 Kafka SCRAM-SASL 认证（支持 SHA256 / SHA512）
// user/pwd 必须非空，否则不启用
func WithSASL(user, pwd string, algo SASLAlgorithm) ProducerAsyncOption {
	return func(cfg *sarama.Config) {
		if user == "" || pwd == "" {
			return
		}
		cfg.Net.SASL.Enable = true
		cfg.Net.SASL.User = user
		cfg.Net.SASL.Password = pwd
		cfg.Net.SASL.Handshake = true

		switch algo {
		case SASL_SHA256:
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
			cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xDGSCRAMClient{HashGeneratorFcn: sHA256}
			}
		case SASL_SHA512:
			cfg.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA512
			cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
				return &xDGSCRAMClient{HashGeneratorFcn: sHA512}
			}
		default:
			panic(fmt.Sprintf("unsupported SASL algorithm: %s", algo))
		}
	}
}

func WithClientID(id string) ProducerAsyncOption {
	return func(cfg *sarama.Config) {
		cfg.ClientID = id
	}
}

// WithPartitioner 设置 Kafka 的分区策略
// hash 保证相同 key 的顺序性；manual 用于自定义分区
func WithPartitioner(strategy PartitionerStrategy) ProducerAsyncOption {
	return func(cfg *sarama.Config) {
		switch strategy {
		case PartitionerRandom:
			cfg.Producer.Partitioner = sarama.NewRandomPartitioner
		case PartitionerHash:
			cfg.Producer.Partitioner = sarama.NewHashPartitioner
		case PartitionerRoundRobin:
			cfg.Producer.Partitioner = sarama.NewRoundRobinPartitioner
			cfg.Producer.Return.Successes = true
		case PartitionerManual:
			cfg.Producer.Partitioner = sarama.NewManualPartitioner
		default:
			panic(fmt.Sprintf("unsupported partitioner strategy: %s", strategy))
		}
	}
}

// WithMessageTimeout 设置生产者发送消息的超时时间
func WithMessageTimeout(timeout time.Duration) ProducerAsyncOption {
	return func(cfg *sarama.Config) {
		cfg.Producer.Timeout = timeout
	}
}
