package main

import (
	"fmt"
	"os"

	"homoscale/internal/homoscale"
)

func main() {
	if err := homoscale.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
