package proxy

import (
	"context"

	"github.com/go-logr/logr"
	icache "github.com/hexiaodai/fence/internal/cache"
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/log"
)

func New(config config.FenceProxy) *Runner {
	r := &Runner{}
	r.config = config
	return r
}

type Runner struct {
	Config
}

func (r *Runner) Name() string {
	return "Runner"
}

type Config struct {
	log    logr.Logger
	config config.FenceProxy
}

func (r *Runner) Start(ctx context.Context) error {
	logger, err := log.NewLogger()
	if err != nil {
		return err
	}
	r.log = logger.WithValues("proxy", r.Name())

	serviceCache := icache.NewService()
	if err := serviceCache.Start(ctx); err != nil {
		return err
	}

	serve, err := NewServe(serviceCache, r.config)
	if err != nil {
		return err
	}

	serve.ListenAndServe(r.config.WormholePort)

	r.log.Info("started")
	return nil
}
