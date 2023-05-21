package config

import (
	"strconv"

	"github.com/hexiaodai/fence/internal/utils"
)

type Config struct {
	IstioNamespace string
	FenceNamespace string
	ProbePort      string
	WormholePort   string
}

type Fence struct {
	Config
	AutoFence     bool
	LogSourcePort string
}

type FenceProxy struct {
	Config
}

func New() Config {
	config := Config{
		IstioNamespace: utils.Lookup("ISTIO_NAMESPACE", "istio-system"),
		FenceNamespace: utils.Lookup("FENCE_NAMESPACE", "fence"),
		ProbePort:      utils.Lookup("PROBE_PORT", "16021"),
		WormholePort:   utils.Lookup("WORMHOLE_PORT", "80"),
	}
	return config
}

func NewFence() Fence {
	autoFence, _ := strconv.ParseBool(utils.Lookup("AUTO_FENCE", "true"))
	config := Fence{
		Config:        New(),
		AutoFence:     autoFence,
		LogSourcePort: utils.Lookup("LOG_SOURCE_PORT", "8082"),
	}
	return config
}

func NewFenceProxy() FenceProxy {
	config := FenceProxy{
		Config: New(),
	}
	return config
}
