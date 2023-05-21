package main

import (
	"fmt"
	"os"

	"github.com/hexiaodai/fence/internal/cmd/proxy"
)

func main() {
	if err := proxy.GetRootCommand().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
