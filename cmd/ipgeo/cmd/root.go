package cmd

import (
	"context"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/clirun"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/spf13/cobra"
)

func buildRootCmd(cfg *config.Config) *cobra.Command {
	var (
		jsonMode   bool
		sourceName string
		inputFile  string
		outputFile string
	)

	root := &cobra.Command{
		Use:           "ipgeo [ip...]",
		Short:         "Resolve IP addresses to geographic information",
		Long:          `ipgeo resolves IPv4 and IPv6 addresses to geographic and network information.`,
		Args:          cobra.ArbitraryArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return clirun.Run(cmd.Context(), clirun.Options{
				Config:     cfg,
				Args:       args,
				JSONMode:   jsonMode,
				SourceName: sourceName,
				InputFile:  inputFile,
				OutputFile: outputFile,
			})
		},
	}

	root.PersistentFlags().BoolVarP(&jsonMode, "json", "j", false, "Output results as JSON")
	root.PersistentFlags().StringVarP(&sourceName, "source", "s", "", "Query only the named source (e.g. GeoLite2)")
	root.Flags().StringVarP(&inputFile, "input", "i", "", "Read input from `file` instead of stdin")
	root.Flags().StringVarP(&outputFile, "output", "o", "", "Write output to `file` instead of stdout")

	root.AddCommand(newInfoCmd(cfg))
	root.AddCommand(newUpdateCmd(cfg))
	if cmd := newUpgradeCmd(cfg); cmd != nil {
		root.AddCommand(cmd)
	}

	return root
}

func Execute(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return buildRootCmd(cfg).ExecuteContext(ctx)
}
