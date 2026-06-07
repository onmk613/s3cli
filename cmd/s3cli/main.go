package main

import (
	"fmt"
	"os"

	"s3cli/pkg/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Printf("Execute Command Failed: %s", err)
		os.Exit(1)
	}
}
