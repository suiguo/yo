package redisx

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

////////////////////////////////////////////////////////////////////////////////
// Redis 配置结构体，支持 JSON / YAML 自动解析
////////////////////////////////////////////////////////////////////////////////

// Config 定义了 Redis 客户端初始化的基本配置参数
// 支持通过 YAML 或 JSON 文件、字符串解析生成
// 可用于单机或集群模式，支持 ACL 与 TLS 配置
type Config struct {
	Addrs     []string `json:"addrs" yaml:"addrs"`           // Redis 地址列表（支持单机或集群）
	Username  string   `json:"username" yaml:"username"`     // Redis 用户名（用于 ACL）
	Password  string   `json:"password" yaml:"password"`     // Redis 密码
	DB        int      `json:"db" yaml:"db"`                 // Redis 数据库编号（单机模式有效）
	UseTLS    bool     `json:"use_tls" yaml:"use_tls"`       // 是否启用 TLS
	IsCluster bool     `json:"is_cluster" yaml:"is_cluster"` // 是否是集群（非必须）
}

////////////////////////////////////////////////////////////////////////////////
// 配置解析功能：支持 string / []byte / file
////////////////////////////////////////////////////////////////////////////////

// LoadFromYAMLOrJSONString 从字符串中自动识别格式（YAML/JSON）并解析配置
func LoadFromYAMLOrJSONString(data string) (*Config, error) {
	return LoadFromYAMLOrJSONBytes([]byte(data))
}

// LoadFromYAMLOrJSONBytes 从字节流中自动识别格式（YAML/JSON）并解析配置
func LoadFromYAMLOrJSONBytes(data []byte) (*Config, error) {
	content := strings.TrimSpace(string(data))
	if strings.HasPrefix(content, "{") {
		return LoadFromJSONBytes(data)
	}
	return LoadFromYAMLBytes(data)
}

// LoadFromJSONBytes 解析 JSON 格式配置
func LoadFromJSONBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}
	return &cfg, nil
}

// LoadFromYAMLBytes 解析 YAML 格式配置
func LoadFromYAMLBytes(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal YAML: %w", err)
	}
	return &cfg, nil
}

// LoadFromYAMLOrJSONFile 根据文件扩展名识别格式并加载配置
func LoadFromYAMLOrJSONFile(filePath string) (*Config, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".yaml", ".yml":
		return loadFromYAML(filePath)
	case ".json":
		return loadFromJSON(filePath)
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}
}

// loadFromJSON 加载 JSON 配置文件
func loadFromJSON(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read JSON file: %w", err)
	}
	return LoadFromJSONBytes(data)
}

// loadFromYAML 加载 YAML 配置文件
func loadFromYAML(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read YAML file: %w", err)
	}
	return LoadFromYAMLBytes(data)
}

////////////////////////////////////////////////////////////////////////////////
// Option 扩展配置方案
////////////////////////////////////////////////////////////////////////////////

// Option 是一个函数类型，用于扩展 UniversalOptions 配置
// 可用于注入 TLS、连接池参数、超时、重试等行为
type Option func(*redis.UniversalOptions)

// WithTLSConfig 注入自定义 TLS 配置
func WithTLSConfig(tlsConfig *tls.Config) Option {
	return func(opts *redis.UniversalOptions) {
		opts.TLSConfig = tlsConfig
	}
}

// WithMaxRetries 设置最大重试次数
func WithMaxRetries(n int) Option {
	return func(opts *redis.UniversalOptions) {
		opts.MaxRetries = n
	}
}

// WithDialTimeout 设置连接超时时间
func WithDialTimeout(d time.Duration) Option {
	return func(opts *redis.UniversalOptions) {
		opts.DialTimeout = d
	}
}

// WithReadTimeout 设置读取超时时间
func WithReadTimeout(d time.Duration) Option {
	return func(opts *redis.UniversalOptions) {
		opts.ReadTimeout = d
	}
}

// WithWriteTimeout 设置写入超时时间
func WithWriteTimeout(d time.Duration) Option {
	return func(opts *redis.UniversalOptions) {
		opts.WriteTimeout = d
	}
}

// WithPoolSize 设置连接池最大连接数
func WithPoolSize(size int) Option {
	return func(opts *redis.UniversalOptions) {
		opts.PoolSize = size
	}
}

// WithMinIdleConns 设置最小空闲连接数
func WithMinIdleConns(n int) Option {
	return func(opts *redis.UniversalOptions) {
		opts.MinIdleConns = n
	}
}

////////////////////////////////////////////////////////////////////////////////
// Redis 客户端初始化
////////////////////////////////////////////////////////////////////////////////

// Init 使用给定配置和扩展 Option 初始化 Redis 客户端
// 如果连接失败将返回错误
func Init(cfg *Config, opts ...Option) (redis.UniversalClient, error) {
	redisOpts := &redis.UniversalOptions{
		Addrs:    cfg.Addrs,
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	// 启用 TLS 默认配置
	if cfg.UseTLS {
		redisOpts.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}
	withResilientDefaults(redisOpts)

	// 应用用户自定义的 Option 设置
	for _, opt := range opts {
		opt(redisOpts)
	}
	if cfg.IsCluster {
		return redis.NewClusterClient(redisOpts.Cluster()), nil
	}
	// 创建 Redis 客户端
	client := redis.NewUniversalClient(redisOpts)

	// Ping 测试连接
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis connect failed: %w", err)
	}

	return client, nil
}

// InitFromFile 直接通过文件路径加载配置并初始化 Redis 客户端
func InitFromFile(filePath string, opts ...Option) (redis.UniversalClient, error) {
	cfg, err := LoadFromYAMLOrJSONFile(filePath)
	if err != nil {
		return nil, err
	}
	return Init(cfg, opts...)
}

func withResilientDefaults(opts *redis.UniversalOptions) {
	opts.MaxRetries = 5
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second
	opts.PoolSize = 10
	opts.MinIdleConns = 2
}
