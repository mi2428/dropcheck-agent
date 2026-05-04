package main

import (
	"context"
	"fmt"
	"os"

	"dropcheck/controller/internal/mcpserver"
)

func main() {
	backend := mcpserver.NewRealBackend(mcpserver.SessionStartOptions{})
	defer backend.Close()
	if err := mcpserver.RunStdio(context.Background(), backend); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
