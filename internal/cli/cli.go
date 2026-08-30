package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/kubewhy/kubewhy/internal/api"
	"github.com/kubewhy/kubewhy/internal/collector"
	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/model"
)

func Run(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "diagnose":
		runDiagnose(args[1:])
	case "serve":
		runServe(args[1:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage()
		os.Exit(2)
	}
}

func runDiagnose(args []string) {
	flags := flag.NewFlagSet("diagnose", flag.ExitOnError)
	file := flags.String("file", "", "JSON request file; reads stdin when omitted")
	asJSON := flags.Bool("json", false, "write the complete report as JSON")
	podRef := flags.String("pod", "", "pod name or pod/name to collect from Kubernetes")
	namespace := flags.String("namespace", "default", "Kubernetes namespace for --pod")
	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig; uses the default when omitted")
	kubeContext := flags.String("context", "", "kubeconfig context for --pod")
	tailLines := flags.Int64("tail", 200, "number of log lines to collect per container")
	previousLogs := flags.Bool("previous", false, "collect previous container logs")
	watch := flags.Bool("watch", false, "repeat pod diagnosis until interrupted")
	interval := flags.Duration("interval", 5*time.Second, "delay between watch collections")
	exitCodes := flags.Bool("exit-code", false, "exit non-zero when the diagnosis is not healthy")
	_ = flags.Parse(args)

	if *podRef != "" {
		if *file != "" {
			fatal(fmt.Errorf("--file and --pod cannot be used together"))
		}
		if *interval <= 0 {
			fatal(fmt.Errorf("--interval must be greater than zero"))
		}
		podName := strings.TrimPrefix(*podRef, "pod/")
		if podName == "" || strings.Contains(podName, "/") {
			fatal(fmt.Errorf("invalid --pod %q; use NAME or pod/NAME", *podRef))
		}
		cluster, err := collector.NewFromKubeconfig(*kubeconfig, *kubeContext)
		if err != nil {
			fatal(err)
		}
		ctx := context.Background()
		stop := func() {}
		if *watch {
			ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
		}
		defer stop()
		code := runClusterDiagnose(ctx, cluster, *namespace, podName, collector.Options{TailLines: *tailLines, PreviousLogs: *previousLogs}, *asJSON, *watch, *interval)
		if *exitCodes {
			os.Exit(code)
		}
		return
	}
	if *watch {
		fatal(fmt.Errorf("--watch requires --pod"))
	}

	var request model.DiagnoseRequest
	data, err := readInput(*file)
	if err != nil {
		fatal(err)
	}
	if err := json.Unmarshal(data, &request); err != nil {
		fatal(fmt.Errorf("decode request: %w", err))
	}
	if request.Pod.Metadata.Name == "" {
		fatal(fmt.Errorf("pod.metadata.name is required"))
	}
	report := diagnosis.NewEngine().Diagnose(request)
	if *asJSON {
		writeIndented(report)
	} else {
		printReport(report)
	}
	if *exitCodes {
		os.Exit(reportExitCode(report))
	}
}

func runClusterDiagnose(ctx context.Context, cluster *collector.Collector, namespace, podName string, options collector.Options, asJSON, watch bool, interval time.Duration) int {
	firstAttempt := true
	lastStatus := "unknown"
	for {
		request, err := cluster.Collect(ctx, namespace, podName, options)
		if err != nil {
			if firstAttempt {
				fatal(err)
			}
			fmt.Fprintf(os.Stderr, "kubewhy: collection failed: %v\n", err)
		} else {
			report := diagnosis.NewEngine().Diagnose(request)
			lastStatus = report.Status
			if asJSON {
				writeJSON(report)
			} else {
				if watch {
					fmt.Printf("\n--- %s ---\n", report.GeneratedAt.Format(time.RFC3339))
				}
				printReport(report)
			}
		}
		firstAttempt = false
		if !watch {
			return reportExitCodeForStatus(lastStatus)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return reportExitCodeForStatus(lastStatus)
		case <-timer.C:
		}
	}
}

// reportExitCodeForStatus is stable for scripts: healthy=0, degraded=1,
// broken=2, and unknown=3. It is opt-in through --exit-code.
func reportExitCodeForStatus(status string) int {
	switch status {
	case "healthy":
		return 0
	case "degraded":
		return 1
	case "broken":
		return 2
	case "unknown":
		return 3
	default:
		return 3
	}
}

func reportExitCode(report model.Report) int {
	return reportExitCodeForStatus(report.Status)
}

func runServe(args []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := flags.String("listen", ":8080", "HTTP listen address")
	_ = flags.Parse(args)
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.NewServer(nil, log.Default()).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
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

func writeJSON(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fatal(err)
	}
}

func printReport(report model.Report) {
	fmt.Printf("%s (confidence: %s)\n", strings.ToUpper(report.Status), report.Confidence)
	fmt.Printf("%s\n", report.Summary)
	if len(report.MissingContext) > 0 {
		fmt.Printf("missing context: %s\n", strings.Join(report.MissingContext, ", "))
	}
	if report.Pod.Namespace != "" {
		fmt.Printf("namespace: %s\n", report.Pod.Namespace)
	}
	if report.Pod.Node != "" {
		fmt.Printf("node: %s\n", report.Pod.Node)
	}
	if report.RootCause != nil {
		fmt.Printf("\nRoot cause: %s (confidence: %s)\n", report.RootCause.Title, report.RootCause.Confidence)
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
	fmt.Println("kubewhy explains why a Kubernetes pod is unhealthy\n\nUsage:\n  kubewhy diagnose --file request.json [--json]\n  kubewhy diagnose --file request.json --json --exit-code\n  kubewhy diagnose --pod pod/api --namespace payments [--previous]\n  kubewhy diagnose --pod pod/api --namespace payments --watch --interval 5s\n  kubewhy serve --listen :8080\n\nPOST /api/v1/diagnose with a DiagnoseRequest to use the API.")
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "kubewhy:", err); os.Exit(1) }
