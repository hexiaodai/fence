package metric

import (
	"context"

	"github.com/hexiaodai/fence/internal/config"
)

func New(server config.Server) *Runner {
	server.Logger = server.Logger.WithName("Runner").WithValues("metric", "Runner")
	return &Runner{Server: server}
}

type Runner struct {
	accessLogSource *AccessLogSource
	config.Server
}

func (r *Runner) Start(ctx context.Context) error {
	accessLogSource, err := NewAccessLogSource(r.LogSourcePort, r.Server)
	if err != nil {
		return err
	}
	if err := accessLogSource.Start(); err != nil {
		return err
	}

	r.accessLogSource = accessLogSource

	r.Logger.Info("started")
	return nil
}

func (r *Runner) RegisterHttpLogEntry(h HttpLogEntry) {
	r.accessLogSource.RegisterHttpLogEntry(h)
}
