package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/platform-service-resource-replicator/cmd/platform-service-resource-replicator/app"
)

func main() {
	ctx := context.Background()
	defer ctx.Done()
	cmd := app.NewResourceReplicatorCommand(ctx)

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
