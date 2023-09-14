package command

import (
	"github.com/spf13/cobra"

	"github.com/anchore/clio"
)

func Add(app clio.Application) *cobra.Command {
	cmd := app.SetupCommand(&cobra.Command{
		Use:   "add",
		Short: "Add a new tool to the configuration",
	})

	cmd.AddCommand(AddGoInstall(app))

	return cmd
}
