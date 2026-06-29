package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/kibaamor/ipgeo/cmd/ipgeo/cmd"
)

func main() {
	code := run()
	os.Exit(code)
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
