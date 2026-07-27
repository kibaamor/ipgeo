//go:build !windows

package cmd

import (
	"context"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/updater"
	"github.com/spf13/cobra"
)

func newUpgradeCmd(ctx context.Context, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade the ipgeo CLI binary",
		RunE: func(_ *cobra.Command, _ []string) error {
			return updater.SelfUpdate(ctx, cfg, version)
		},
	}
}
