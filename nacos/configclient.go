package nacos

import (
	"errors"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// Config 接口定义对配置客户端的统一访问方式。
type Config interface {
	Base() config_client.IConfigClient
	GetConfig(dataId, group string) (string, error)
	ListenConfig(dataId, group string, onChange func(data string)) error
	CancelListen(dataId, group string) error
	PublishConfig(dataId, group, content string) (bool, error)
	RemoveConfig(dataId, group string) (bool, error)
}

// configClient 封装 nacos 的配置客户端，提供简化操作接口。
type configClient struct {
	base config_client.IConfigClient
}

// Base 返回底层原始的 IConfigClient 实例。
func (c *configClient) Base() config_client.IConfigClient {
	return c.base
}

// NewConfigClient 创建一个新的配置客户端。
func NewConfigClient(param *vo.NacosClientParam) (Config, error) {
	if param == nil {
		return nil, errors.New("nacos config client param is nil")
	}
	cli, err := clients.NewConfigClient(*param)
	if err != nil {
		return nil, err
	}
	return &configClient{base: cli}, nil
}

// GetConfig 获取指定 dataId 和 group 的配置内容。
func (c *configClient) GetConfig(dataId, group string) (string, error) {
	content, err := c.base.GetConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
	})
	if err != nil {
		log.Error("[nacos-config] get config failed", "dataId", dataId, "group", group, "err", err)
		return "", err
	}
	return content, nil
}

// ListenConfig 注册配置监听器，配置变化时触发回调。
func (c *configClient) ListenConfig(dataId, group string, onChange func(data string)) error {
	return c.base.ListenConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
		OnChange: func(namespace, group, dataId, data string) {
			log.Info("[nacos-config] config changed", "dataId", dataId, "group", group)
			onChange(data)
		},
	})
}

// CancelListen 取消监听指定配置。
func (c *configClient) CancelListen(dataId, group string) error {
	return c.base.CancelListenConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
	})
}

// PublishConfig 发布配置。
func (c *configClient) PublishConfig(dataId, group, content string) (bool, error) {
	ok, err := c.base.PublishConfig(vo.ConfigParam{
		DataId:  dataId,
		Group:   group,
		Content: content,
	})
	if err != nil {
		log.Error("[nacos-config] publish failed", "dataId", dataId, "group", group, "err", err)
		return false, err
	}
	return ok, nil
}

// RemoveConfig 删除配置。
func (c *configClient) RemoveConfig(dataId, group string) (bool, error) {
	return c.base.DeleteConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
	})
}

// GenerateMinimalConfigParam 快速创建 NacosClientParam，复用 discovery 的生成函数。
func GenerateMinimalConfigParam(space_id, username, password string, tls *constant.TLSConfig, hosts []string, grpcPorts []uint64) (*vo.NacosClientParam, error) {
	return GenerateMinimalConfig(space_id, username, password, tls, hosts, grpcPorts)
}
