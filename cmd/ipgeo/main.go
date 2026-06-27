package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
