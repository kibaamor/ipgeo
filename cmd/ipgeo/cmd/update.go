package cmd

import (
	"context"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/updater"
	"github.com/spf13/cobra"
)

func newUpdateCmd(ctx context.Context, cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update source database files",
		RunE: func(_ *cobra.Command, _ []string) error {
			return updater.UpdateAll(ctx, cfg)
		},
	}
}
