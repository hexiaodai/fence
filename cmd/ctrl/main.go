package main

import (
	"fmt"
	"os"

	"github.com/hexiaodai/fence/internal/cmd/ctrl"
)

func main() {
	if err := ctrl.GetRootCommand().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
