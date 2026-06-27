//go:build !windows

package cmd

import (
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/updater"
	"github.com/spf13/cobra"
)

func newUpgradeCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the ipgeo CLI binary",
		RunE: func(cmd *cobra.Command, args []string) error {
			return updater.SelfUpdate(cmd.Context(), cfg, version)
		},
	}
}
