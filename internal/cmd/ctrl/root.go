package ctrl

import "github.com/spf13/cobra"

func GetRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fence controller",
		Short: "Fence Controller",
		Long:  "Fence Controller",
	}

	cmd.AddCommand(getServerCommand())

	return cmd
}
