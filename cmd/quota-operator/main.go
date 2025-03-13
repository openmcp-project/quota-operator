package main

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/quota-operator/cmd/quota-operator/app"
)

func main() {
	ctx := context.Background()
	defer ctx.Done()
	cmd := app.NewQuotaOperatorCommand(ctx)

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
