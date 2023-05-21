package proxy

import (
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/healthz"
	httpproxy "github.com/hexiaodai/fence/internal/proxy"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

func getServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "proxy",
		Aliases: []string{"proxy"},
		Short:   "Fence Proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			return server()
		},
	}

	return cmd
}

func server() error {
	return setupRunners()
}

func setupRunners() error {
	ctx := ctrl.SetupSignalHandler()

	config := config.NewFenceProxy()

	proxyrunner := httpproxy.New(config)
	if err := proxyrunner.Start(ctx); err != nil {
		return err
	}

	healthzRunner := healthz.New(config.Config)
	if err := healthzRunner.Start(); err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}
