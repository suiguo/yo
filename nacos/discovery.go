package nacos

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"github.com/suiguo/yo/logger"
	"gopkg.in/yaml.v3"
)

type ConfigClientConfig struct {
	NamespaceId string              `json:"namespace" yaml:"namespace"`
	Username    string              `json:"username" yaml:"username"`
	Password    string              `json:"password" yaml:"password"`
	Hosts       []string            `json:"hosts" yaml:"hosts"`           // 支持 host:port 或 schema://host:port
	GrpcPorts   []uint64            `json:"grpc_ports" yaml:"grpc_ports"` // 可选
	TLS         *constant.TLSConfig `json:"tls" yaml:"tls"`               // 可选
}

// LoadConfigClientConfigFromFile 加载 JSON 或 YAML 配置文件
func LoadConfigClientConfigFromFile(path string) (*ConfigClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file error: %w", err)
	}

	var cfg ConfigClientConfig
	switch {
	case strings.HasSuffix(path, ".json"):
		err = json.Unmarshal(data, &cfg)
	case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
		err = yaml.Unmarshal(data, &cfg)
	default:
		return nil, fmt.Errorf("unsupported config file type: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("unmarshal config failed: %w", err)
	}
	return &cfg, nil
}

// 初始化日志实例
var log = logger.GetLogger("nacos")

// SetLogger 可用于替换默认日志实现
func SetLogger(l logger.WarpLog) {
	if l != nil {
		log = l
	}
}

// Discovery 接口封装了服务发现相关操作
// 便于后续 mock 或扩展

type Discovery interface {
	RegisterInstance(service_name, group_name, cluster_name, host string, port int, weight int, metadata map[string]string) (bool, error)
	GetOneInstance(service_name, groupName string, clusters []string) (*model.Instance, error)
	GetOneServer(service_name, group_name string) (string, error)
	GetOneServerWithCluster(service_name, group_name string, clusters []string) (string, error)
	GetService(service_name, group_name string) (model.Service, error)
	GetServiceWithCluster(service_name, group_name string, clusters []string) (model.Service, error)
	Subscribe(service_name, group string, clusters []string, cb func([]model.Instance, error)) error
	Unsubscribe(service_name, group string, clusters []string) error
	GetAllServicesInfo(ns, group string) []string
	Base() naming_client.INamingClient
}

// discoveryClient 封装了 Nacos 的服务注册与发现功能
// 包装底层 naming_client.INamingClient 接口
// 提供简化、结构化的服务注册、获取、订阅等方法

type discoveryClient struct {
	base naming_client.INamingClient
}

// Base 返回原始的底层 client，可用于 SDK 原生调用
func (d *discoveryClient) Base() naming_client.INamingClient {
	return d.base
}

// RegisterInstance 注册服务实例到 Nacos
func (d *discoveryClient) RegisterInstance(service_name, group_name, cluster_name, host string, port int, weight int, metadata map[string]string) (bool, error) {
	if cluster_name == "" {
		cluster_name = "DEFAULT"
	}
	return d.base.RegisterInstance(vo.RegisterInstanceParam{
		Ip:          host,
		Port:        uint64(port),
		Weight:      float64(weight),
		Enable:      true,
		Healthy:     true,
		ServiceName: service_name,
		GroupName:   group_name,
		ClusterName: cluster_name,
		Metadata:    metadata,
		Ephemeral:   true,
	})
}

// GetOneInstance 获取一个健康实例（含集群信息）
func (d *discoveryClient) GetOneInstance(service_name, groupName string, clusters []string) (*model.Instance, error) {
	ins, err := d.base.SelectOneHealthyInstance(vo.SelectOneHealthInstanceParam{
		ServiceName: service_name,
		GroupName:   groupName,
		Clusters:    clusters,
	})
	if err != nil {
		return nil, err
	}
	return ins, nil
}

// GetOneServer 获取一个健康实例地址（默认不带集群）
func (d *discoveryClient) GetOneServer(service_name, group_name string) (string, error) {
	return d.GetOneServerWithCluster(service_name, group_name, nil)
}

// GetOneServerWithCluster 获取一个健康实例地址（可指定集群）
func (d *discoveryClient) GetOneServerWithCluster(service_name, group_name string, clusters []string) (string, error) {
	ins, err := d.GetOneInstance(service_name, group_name, clusters)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", ins.Ip, ins.Port), nil
}

// GetService 获取服务元信息（默认不带集群）
func (d *discoveryClient) GetService(service_name, group_name string) (model.Service, error) {
	return d.GetServiceWithCluster(service_name, group_name, nil)
}

// GetServiceWithCluster 获取服务元信息（支持指定集群）
func (d *discoveryClient) GetServiceWithCluster(service_name, group_name string, clusters []string) (model.Service, error) {
	return d.base.GetService(vo.GetServiceParam{
		ServiceName: service_name,
		GroupName:   group_name,
		Clusters:    clusters,
	})
}

// Subscribe 订阅服务变更通知（用于服务监听）
func (d *discoveryClient) Subscribe(service_name, group string, clusters []string, cb func(services []model.Instance, err error)) error {
	return d.base.Subscribe(&vo.SubscribeParam{
		ServiceName:       service_name,
		GroupName:         group,
		Clusters:          clusters,
		SubscribeCallback: cb,
	})
}

// Unsubscribe 取消服务订阅
func (d *discoveryClient) Unsubscribe(service_name, group string, clusters []string) error {
	return d.base.Unsubscribe(&vo.SubscribeParam{
		ServiceName: service_name,
		GroupName:   group,
		Clusters:    clusters,
	})
}

// NewDiscoveryClient 创建 discoveryClient 实例
func NewDiscoveryClient(param *vo.NacosClientParam) (Discovery, error) {
	if param == nil {
		return nil, fmt.Errorf("param is nil")
	}
	cli, err := clients.NewNamingClient(*param)
	if err != nil {
		return nil, err
	}
	return &discoveryClient{cli}, nil
}

func GenerateMinimalConfigByCfg(cfg *ConfigClientConfig) (*vo.NacosClientParam, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	return GenerateMinimalConfig(cfg.NamespaceId, cfg.Username, cfg.Password, cfg.TLS, cfg.Hosts, cfg.GrpcPorts)
}
func GenerateMinimalConfigByFile(path string) (*vo.NacosClientParam, error) {
	cfg, err := LoadConfigClientConfigFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load config from file failed: %w", err)
	}
	return GenerateMinimalConfigByCfg(cfg)
}

// GenerateMinimalConfig 构建简洁的 Nacos 配置参数
// 支持 TLS、多个主机、多端口配置
func GenerateMinimalConfig(space_id, username, password string, tls *constant.TLSConfig, hosts []string, grpcPorts []uint64) (*vo.NacosClientParam, error) {
	if len(hosts) == 0 {
		log.Error("[nacos]", "❌ host list is empty", hosts)
		return nil, fmt.Errorf("host list is empty")
	}

	if len(grpcPorts) > 0 && len(grpcPorts) != len(hosts) {
		log.Error("[nacos]", "grpcPorts", grpcPorts, "hosts", hosts, "err", "grpcPorts and hosts length mismatch")
		return nil, fmt.Errorf("grpcPorts and hosts length mismatch")
	}

	clientCfg := constant.NewClientConfig(
		constant.WithNamespaceId(space_id),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("./nacosx/log"),
		constant.WithCacheDir("./nacosx/cache"),
		constant.WithLogLevel("info"),
		constant.WithUsername(username),
		constant.WithPassword(password),
	)
	if tls != nil {
		constant.WithTLS(*tls)(clientCfg)
	}

	var serverConfigs []constant.ServerConfig
	for i, h := range hosts {
		if !strings.Contains(h, "://") {
			h = "http://" + h
		}
		raw, err := url.Parse(h)
		if err != nil {
			log.Warning("[nacos]", "⚠️ invalid host", h, "err", err)
			continue
		}

		scheme := raw.Scheme
		if scheme == "" {
			scheme = "http"
		}
		if tls != nil {
			scheme = "https"
		}

		if raw.Path == "" {
			raw.Path = "/nacos"
		}

		port := uint64(8848)
		if p := raw.Port(); p != "" {
			if parsed, err := strconv.ParseUint(p, 10, 64); err == nil {
				port = parsed
			} else {
				log.Warning("[nacos]", "⚠️ invalid port in host", h, "err", err)
				continue
			}
		}

		serverConfig := constant.NewServerConfig(
			raw.Hostname(),
			port,
			constant.WithScheme(scheme),
			constant.WithContextPath(raw.Path),
		)
		if len(grpcPorts) > 0 {
			serverConfig.GrpcPort = grpcPorts[i]
		}

		serverConfigs = append(serverConfigs, *serverConfig)
	}

	return &vo.NacosClientParam{
		ClientConfig:  clientCfg,
		ServerConfigs: serverConfigs,
	}, nil
}
func (d *discoveryClient) GetAllServicesInfo(ns, group string) []string {
	page := uint32(0)
	services := make([]string, 0)
	for {
		page++
		list, err := d.base.GetAllServicesInfo(vo.GetAllServiceInfoParam{
			NameSpace: ns,
			GroupName: group,
			PageNo:    page,
			PageSize:  20,
		})
		if err != nil || len(list.Doms) == 0 {
			break
		}
		services = append(services, list.Doms...)
	}
	return services
}
