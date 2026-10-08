// Command phantom-mail is a receive-only disposable inbox service: an SMTP
// receiver, a JSON API, and a small web interface in one process.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"phantom-mail/internal/app"
	"phantom-mail/internal/config"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "healthcheck":
			return healthcheckCommand(os.Getenv)
		case "serve":
		default:
			fmt.Fprintf(os.Stderr, "usage: phantom-mail [serve|healthcheck]\nunknown command %q\n", args[0])
			return 2
		}
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error:\n%v\n", err)
		return 2
	}
	log := app.NewLogger(os.Stdout, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, cfg, log); err != nil {
		log.Error("exited with error", "error", err.Error())
		return 1
	}
	return 0
}
