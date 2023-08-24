package proxy

import (
	"context"

	icache "github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
)

func New(server config.Server) *Runner {
	server.Logger = server.Logger.WithName("Runner").WithValues("proxy", "Runner")
	return &Runner{Server: server}
}

type Runner struct {
	config.Server
}

func (r *Runner) Start(ctx context.Context) error {
	serviceCache := icache.NewService(r.Server)
	if err := serviceCache.Start(ctx); err != nil {
		return err
	}

	serve, err := NewServe(serviceCache, r.Server)
	if err != nil {
		return err
	}

	serve.ListenAndServe(r.WormholePort)

	r.Logger.Info("started")
	return nil
}
