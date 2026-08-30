package main

import (
	"os"

	"github.com/kubewhy/kubewhy/internal/cli"
)

func main() {
	cli.Run(os.Args[1:])
}
