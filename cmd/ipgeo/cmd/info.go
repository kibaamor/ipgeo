package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/config"
	"github.com/kibaamor/ipgeo/cmd/ipgeo/internal/sources"
	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func newInfoCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show version and configuration information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Version:    %s\n", Version)
			fmt.Printf("Commit:     %s\n", Commit)
			fmt.Printf("Build Date: %s\n", BuildDate)
			fmt.Println("License:    Apache License 2.0")
			fmt.Printf("Home Dir:   %s\n", cfg.HomeDir())
			fmt.Printf("Config:     %s\n", filepath.Join(cfg.HomeDir(), "config.yaml"))
			fmt.Println()
			fmt.Println("Sources:")

			for _, source := range sources.Describe(cfg.Sources, cfg.SourcePath) {
				fmt.Printf("  - name:   %s\n", source.Name)
				fmt.Printf("    type:   %s\n", source.Type)
				fmt.Printf("    file:   %s\n", source.File)
				fmt.Printf("    path:   %s\n", source.Path)
				fmt.Printf("    exists: %s\n", fileExists(source.Path))
				if source.Companion != nil {
					fmt.Printf("    companion file:   %s\n", source.Companion.File)
					fmt.Printf("    companion path:   %s\n", source.Companion.Path)
					fmt.Printf("    companion exists: %s\n", fileExists(source.Companion.Path))
				}
			}

			return nil
		},
	}
}

func fileExists(path string) string {
	if _, err := os.Stat(path); err == nil {
		return "yes"
	}
	return "no"
}
