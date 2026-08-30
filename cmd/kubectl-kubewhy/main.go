package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kubewhy/kubewhy/internal/cli"
)

func main() {
	args, err := normalizeArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "kubectl-kubewhy:", err)
		os.Exit(2)
	}
	cli.Run(args)
}

func normalizeArgs(args []string) ([]string, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return []string{"help"}, nil
	}
	if args[0] == "serve" {
		return args, nil
	}

	result := []string{"diagnose"}
	start := 0
	if args[0] == "diagnose" {
		start = 1
	}
	for i := start; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "pod":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, fmt.Errorf("pod resource name is required after %q", arg)
			}
			i++
			result = append(result, "--pod", "pod/"+args[i])
		case strings.HasPrefix(arg, "pod/"):
			if arg == "pod/" {
				return nil, fmt.Errorf("pod name is required after pod/")
			}
			result = append(result, "--pod", arg)
		case arg == "-n" || arg == "--namespace":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, fmt.Errorf("namespace is required after %q", arg)
			}
			i++
			result = append(result, "--namespace", args[i])
		case strings.HasPrefix(arg, "-n="):
			result = append(result, "--namespace", strings.TrimPrefix(arg, "-n="))
		case strings.HasPrefix(arg, "--namespace="):
			result = append(result, "--namespace", strings.TrimPrefix(arg, "--namespace="))
		case arg == "--pod":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("pod name is required after --pod")
			}
			i++
			result = append(result, "--pod", args[i])
		default:
			result = append(result, arg)
		}
	}
	return result, nil
}
