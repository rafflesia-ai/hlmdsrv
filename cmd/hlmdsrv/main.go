package main

import (
	"context"
	"os"

	"github.com/rafflesia-ai/hlmdsrv/internal/mdsrvcli"
)

func main() {
	if err := mdsrvcli.Execute(context.Background(), os.Args[1:]); err != nil {
		os.Exit(mdsrvcli.ExitCode(err))
	}
}
