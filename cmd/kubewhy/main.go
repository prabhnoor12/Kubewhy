package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/kubewhy/kubewhy/internal/api"
	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/model"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "diagnose":
		runDiagnose(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runDiagnose(args []string) {
	flags := flag.NewFlagSet("diagnose", flag.ExitOnError)
	file := flags.String("file", "", "JSON request file; reads stdin when omitted")
	asJSON := flags.Bool("json", false, "write the complete report as JSON")
	_ = flags.Parse(args)
	data, err := readInput(*file)
	if err != nil {
		fatal(err)
	}
	var request model.DiagnoseRequest
	if err := json.Unmarshal(data, &request); err != nil {
		fatal(fmt.Errorf("decode request: %w", err))
	}
	if request.Pod.Metadata.Name == "" {
		fatal(fmt.Errorf("pod.metadata.name is required"))
	}
	report := diagnosis.NewEngine().Diagnose(request)
	if *asJSON {
		writeIndented(report)
		return
	}
	printReport(report)
}

func runServe(args []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := flags.String("listen", ":8080", "HTTP listen address")
	_ = flags.Parse(args)
	server := &http.Server{Addr: *listen, Handler: api.NewServer(nil, log.Default()).Handler()}
	log.Printf("kubewhy listening on %s", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}

func readInput(file string) ([]byte, error) {
	if file == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(file)
}
func writeIndented(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fatal(err)
	}
}
func printReport(report model.Report) {
	fmt.Printf("%s\n", strings.ToUpper(report.Status))
	fmt.Printf("%s\n", report.Summary)
	if report.Pod.Namespace != "" {
		fmt.Printf("namespace: %s\n", report.Pod.Namespace)
	}
	if report.Pod.Node != "" {
		fmt.Printf("node: %s\n", report.Pod.Node)
	}
	for _, reason := range report.Reasons {
		fmt.Printf("\n[%s] %s (%s)\n", strings.ToUpper(reason.Severity), reason.Title, reason.Code)
		fmt.Printf("  %s\n", reason.Explanation)
		if len(reason.Evidence) > 0 {
			fmt.Printf("  evidence: %s\n", strings.Join(reason.Evidence, "; "))
		}
		if len(reason.Remediation) > 0 {
			fmt.Printf("  next: %s\n", strings.Join(reason.Remediation, "; "))
		}
	}
	if len(report.Containers) > 0 {
		fmt.Println("\nContainers:")
		for _, container := range report.Containers {
			fmt.Printf("  - %s (%s): %s ready=%t restarts=%d — %s\n", container.Name, container.Kind, container.State, container.Ready, container.RestartCount, strings.Join(container.Details, "; "))
		}
	}
	if len(report.RelevantEvents) > 0 {
		fmt.Println("\nRelevant events:")
		for _, event := range report.RelevantEvents {
			fmt.Printf("  - %s: %s (count=%d)\n", event.Reason, event.Message, event.Count)
		}
	}
}
func usage() {
	fmt.Println("kubewhy explains why a Kubernetes pod is unhealthy\n\nUsage:\n  kubewhy diagnose --file request.json [--json]\n  kubewhy serve --listen :8080\n\nPOST /api/v1/diagnose with a DiagnoseRequest to use the API.")
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "kubewhy:", err); os.Exit(1) }
