package cmd

import (
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/updater"
	"github.com/spf13/cobra"
)

func newUpdateCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update source database files",
		RunE: func(cmd *cobra.Command, args []string) error {
			return updater.UpdateAll(cmd.Context(), cfg)
		},
	}
}
