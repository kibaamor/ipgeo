//go:build windows

package cmd

import (
	"context"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/spf13/cobra"
)

func newUpgradeCmd(_ context.Context, cfg *config.Config) *cobra.Command {
	return nil
}