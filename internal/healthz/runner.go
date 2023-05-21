package healthz

import (
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/log"
)

func New(config config.Config) *Runner {
	r := &Runner{}
	r.config = config
	return r
}

type Runner struct {
	Config
}

type Config struct {
	healthz *healthz
	log     logr.Logger
	config  config.Config
}

func (r *Runner) Name() string {
	return "Runner"
}

func (r *Runner) Start() error {
	logger, err := log.NewLogger()
	if err != nil {
		return err
	}
	r.log = logger.WithValues("healthz", r.Name())

	if r.config.ProbePort == r.config.WormholePort {
		return fmt.Errorf("health check port is conflict with wormholePort, conflict port is %v", r.config.ProbePort)
	}
	addr := fmt.Sprintf(":%v", r.config.ProbePort)
	go func() {
		if err := http.ListenAndServe(addr, r.healthz); err != nil {
			r.log.Error(err, "failed to start health check listener", "addr", r.config.ProbePort)
			return
		}
	}()

	r.log.Info("started")
	return nil
}

type healthz struct{}

func (h *healthz) ServeHTTP(w http.ResponseWriter, req *http.Request) {}
