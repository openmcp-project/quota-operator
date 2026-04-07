package main

import (
	"fmt"
	"os"

	"github.com/openmcp-project/quota-operator/cmd/quota-operator/app"
)

func main() {
	cmd := app.NewPlatformServiceQuotaCommand()

	if err := cmd.Execute(); err != nil {
		fmt.Print(err)
		os.Exit(1)
	}
}
