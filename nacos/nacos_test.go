package nacos

import "testing"

func TestNacos(t *testing.T) {
	cfg, err := GenerateMinimalConfigByFile("nacos.yaml")
	if err != nil {
		log.Panic("nacos config error", "err", err)
	}
	configCli, err := NewConfigClient(cfg)
	if err != nil {
		log.Panic("nacos client error", "err", err)
	}
	data, err := configCli.GetConfig("config", "tg_sport")
	log.Info("nacos config", "data", data, "err", err)
	configCli.ListenConfig("config", "tg_sport", func(data string) {
		log.Info("nacos config changed", "data", data)
	})
}
