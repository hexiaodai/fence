package config

import (
	"strconv"

	"github.com/hexiaodai/fence/internal/logging"
	"github.com/hexiaodai/fence/internal/utils"
)

const (
	SidecarFenceLabel        = "sidecar.fence.io"
	SidecarFenceValueEnabled = "enabled"
	SidecarFenceValueDisable = "disable"
)

// Server wraps the Fence configuration and additional parameters
// used by Fence server.
type Server struct {
	// FenceNamespace is the namespace that Fence runs in.
	FenceNamespace string
	// IstioNamespace is the namespace that Istio runs in.
	IstioNamespace string
	// ProbePort is the health check port.
	ProbePort string
	// WormholePort is the wormhole port.
	WormholePort string
	// AutoFence is an automatic management sidecar.
	AutoFence bool
	// LogSourcePort is the LogSource port.
	LogSourcePort string
	// Logger is the logr implementation used by Fence.
	Logger logging.Logger
}

// New returns a Server with default parameters.
func New() Server {
	autoFence, _ := strconv.ParseBool(utils.Lookup("AUTO_FENCE", "true"))
	return Server{
		FenceNamespace: utils.Lookup("FENCE_NAMESPACE", "fence"),
		IstioNamespace: utils.Lookup("ISTIO_NAMESPACE", "istio-system"),
		ProbePort:      utils.Lookup("PROBE_PORT", "16021"),
		WormholePort:   utils.Lookup("WORMHOLE_PORT", "80"),
		AutoFence:      autoFence,
		LogSourcePort:  utils.Lookup("LOG_SOURCE_PORT", "8082"),
		// the default logger
		Logger: logging.DefaultLogger(logging.LogLevelInfo),
	}
}
