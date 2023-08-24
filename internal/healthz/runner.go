package healthz

import (
	"fmt"
	"net/http"

	"github.com/hexiaodai/fence/internal/config"
)

func New(server config.Server) *Runner {
	server.Logger = server.Logger.WithName("Runner").WithValues("healthz", "Runner")
	return &Runner{Server: server}
}

type Runner struct {
	healthz *healthz
	config.Server
}

func (r *Runner) Start() error {
	if r.ProbePort == r.WormholePort {
		return fmt.Errorf("health check port is conflict with wormholePort. conflict port is %v", r.ProbePort)
	}
	addr := fmt.Sprintf(":%v", r.ProbePort)
	go func() {
		if err := http.ListenAndServe(addr, r.healthz); err != nil {
			r.Logger.Error(err, "failed to start health check listener", "addr", r.ProbePort)
			return
		}
	}()

	r.Logger.Info("started")
	return nil
}

type healthz struct{}

func (h *healthz) ServeHTTP(w http.ResponseWriter, req *http.Request) {}
