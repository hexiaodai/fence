package ctrl

import (
	"github.com/hexiaodai/fence/internal/config"
	"github.com/hexiaodai/fence/internal/controller"
	"github.com/hexiaodai/fence/internal/healthz"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

func getServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "controller",
		Aliases: []string{"ctrl", "controller"},
		Short:   "Fence Controller",
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

	server := config.New()

	ctrlrunner := controller.New(server)
	if err := ctrlrunner.Start(ctx); err != nil {
		return err
	}

	healthzRunner := healthz.New(server)
	if err := healthzRunner.Start(); err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}
