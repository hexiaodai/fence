package metric

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/log"
)

func New(config config.Fence) *Runner {
	r := &Runner{}
	r.config = config
	return r
}

type Runner struct {
	Config
}

type Config struct {
	accessLogSource *AccessLogSource
	log             logr.Logger
	config          config.Fence
}

func (r *Runner) Name() string {
	return "Runner"
}

func (r *Runner) Start(ctx context.Context) error {
	logger, err := log.NewLogger()
	if err != nil {
		return err
	}
	r.log = logger.WithValues("metric", r.Name())

	accessLogSource, err := NewAccessLogSource(r.config.LogSourcePort)
	if err != nil {
		return err
	}
	if err := accessLogSource.Start(); err != nil {
		return err
	}

	r.accessLogSource = accessLogSource

	r.log.Info("started")
	return nil
}

func (r *Runner) RegisterHttpLogEntry(h HttpLogEntry) {
	r.accessLogSource.RegisterHttpLogEntry(h)
}
