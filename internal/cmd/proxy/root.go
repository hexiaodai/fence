package proxy

import "github.com/spf13/cobra"

func GetRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fence proxy",
		Short: "Fence Proxy",
		Long:  "Fence Proxy",
	}

	cmd.AddCommand(getServerCommand())

	return cmd
}
