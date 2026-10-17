package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/tesserix/australis/internal/adapter/mcp"
)

func main() {
	credential := flag.String("credential-file", "", "private file materialized from the exact OpenBao client path")
	client := flag.String("client-id", "onboarding-mcp-claude-prod", "dedicated Zitadel machine client")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := mcp.RunOnboardingBridge(ctx, *credential, *client); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
