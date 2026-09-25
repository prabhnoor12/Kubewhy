package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/kubewhy/kubewhy/internal/api"
	"github.com/kubewhy/kubewhy/internal/collector"
	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/history"
	"github.com/kubewhy/kubewhy/internal/llm"
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
	case "history":
		runHistory(args[1:])
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
	timeout := flags.Duration("timeout", 30*time.Second, "per-collection timeout for Kubernetes API calls")
	exitCodes := flags.Bool("exit-code", false, "exit non-zero when the diagnosis is not healthy")
	explain := flags.Bool("explain", false, "explain the diagnosis with an LLM; in watch mode, explains on state changes only")
	llmURL := flags.String("llm-url", "", "LLM API base URL; overrides KUBEWHY_LLM_BASE_URL")
	llmKey := flags.String("llm-key", "", "LLM API key; overrides KUBEWHY_LLM_API_KEY")
	llmModel := flags.String("llm-model", "", "LLM model name; overrides KUBEWHY_LLM_MODEL")
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
		code := runClusterDiagnose(ctx, cluster, *namespace, podName, collector.Options{TailLines: *tailLines, PreviousLogs: *previousLogs}, *asJSON, *watch, *interval, *timeout, explainConfig{enabled: *explain, url: *llmURL, key: *llmKey, model: *llmModel})
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
	writeHistory(report)
	if *asJSON {
		writeIndented(report)
	} else {
		printReport(report)
	}
	if *explain {
		explainReport(report, explainConfig{enabled: true, url: *llmURL, key: *llmKey, model: *llmModel})
	}
	if *exitCodes {
		os.Exit(reportExitCode(report))
	}
}

type explainConfig struct {
	enabled bool
	url     string
	key     string
	model   string
}

func runClusterDiagnose(ctx context.Context, cluster *collector.Collector, namespace, podName string, options collector.Options, asJSON, watch bool, interval, timeout time.Duration, explain explainConfig) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	firstAttempt := true
	lastStatus := "unknown"
	backoff := interval
	maxBackoff := 5 * time.Minute
	var tracker *stateTracker
	if watch {
		tracker = newStateTracker()
	}
	for {
		collectCtx, cancel := context.WithTimeout(ctx, timeout)
		request, err := cluster.Collect(collectCtx, namespace, podName, options)
		cancel()
		if err != nil {
			if firstAttempt {
				fatal(err)
			}
			logger.Warn("collection failed, retrying with backoff",
				slog.String("namespace", namespace),
				slog.String("pod", podName),
				slog.String("error", err.Error()),
				slog.Duration("backoff", backoff),
			)
		} else {
			backoff = interval
			report := diagnosis.NewEngine().Diagnose(request)
			lastStatus = report.Status
			var event *ChangeEvent
			if tracker != nil {
				event = tracker.track(report.Status, time.Now())
			}
			writeHistory(report)
			if asJSON {
				writeJSON(report)
			} else {
				if watch {
					fmt.Printf("\n--- %s ---\n", report.GeneratedAt.Format(time.RFC3339))
				}
				printReport(report)
			}
			shouldExplain := !watch
			if watch && event != nil {
				fmt.Fprintln(os.Stderr, formatChange(*event, report))
				shouldExplain = true
			}
			if explain.enabled && shouldExplain {
				explainReport(report, explain)
			}
		}
		firstAttempt = false
		if !watch {
			return reportExitCodeForStatus(lastStatus)
		}
		delay := backoff
		if !watch || err == nil {
			delay = interval
		} else {
			jitter := time.Duration(rand.Int63n(int64(delay) / 2))
			delay = delay/2 + jitter
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return reportExitCodeForStatus(lastStatus)
		case <-timer.C:
			if err != nil {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
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
	shutdownTimeout := flags.Duration("shutdown-timeout", 10*time.Second, "maximum time to wait for in-flight requests to drain")
	_ = flags.Parse(args)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.NewServer(nil, logger).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger.Info("kubewhy starting", slog.String("listen", *listen))

	errs := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- err
		}
		close(errs)
	}()

	select {
	case err := <-errs:
		if err != nil {
			fatal(err)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed, forcing exit", slog.String("error", err.Error()))
			os.Exit(1)
		}
		logger.Info("server stopped cleanly")
	}
}

func readInput(file string) ([]byte, error) {
	if file == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(file)
}

var (
	historyOnce  sync.Once
	historyStore *history.Store
)

// writeHistory persists a diagnosis to the local history file. It is
// best-effort: failures never interrupt the diagnosis itself.
func writeHistory(report model.Report) {
	historyOnce.Do(func() {
		store, err := history.NewStore("")
		if err != nil {
			return
		}
		historyStore = store
	})
	if historyStore == nil {
		return
	}
	_ = historyStore.Append(history.Entry{
		Timestamp: report.GeneratedAt.UTC(),
		Report:    report,
		Pod:       history.PodRef{Name: report.Pod.Name, Namespace: report.Pod.Namespace},
		Status:    report.Status,
	})
}

func explainReport(report model.Report, cfg explainConfig) {
	config := llm.ConfigFromEnv()
	if cfg.url != "" {
		config.BaseURL = cfg.url
	}
	if cfg.key != "" {
		config.APIKey = cfg.key
	}
	if cfg.model != "" {
		config.Model = cfg.model
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	explanation, err := llm.NewClient(config).Chat(ctx, llm.BuildPrompt(report))
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubewhy: LLM explanation failed: %v\n", err)
		return
	}
	fmt.Println("\n--- LLM Explanation ---")
	fmt.Println(explanation)
}

func runHistory(args []string) {
	flags := flag.NewFlagSet("history", flag.ExitOnError)
	pod := flags.String("pod", "", "filter by pod name")
	namespace := flags.String("namespace", "", "filter by namespace")
	since := flags.Duration("since", 24*time.Hour, "show entries newer than this duration ago")
	status := flags.String("status", "", "filter by status (healthy/degraded/broken/unknown)")
	last := flags.Int("last", 0, "show only the last N matching entries")
	asJSON := flags.Bool("json", false, "output entries as JSON lines")
	_ = flags.Parse(args)

	store, err := history.NewStore("")
	if err != nil {
		fatal(err)
	}
	entries, err := store.Query(history.Filter{
		Pod:       *pod,
		Namespace: *namespace,
		Status:    *status,
		Since:     time.Now().Add(-*since),
		Last:      *last,
	})
	if err != nil {
		fatal(err)
	}
	if *asJSON {
		for _, entry := range entries {
			writeJSON(entry)
		}
		return
	}
	if len(entries) == 0 {
		fmt.Println("no history entries found")
		return
	}
	for _, entry := range entries {
		printHistoryEntry(entry)
	}
}

func printHistoryEntry(entry history.Entry) {
	rootCause := "-"
	if entry.Report.RootCause != nil {
		rootCause = entry.Report.RootCause.Code
	}
	fmt.Printf("%s  %-25s %-8s %s (%s confidence)\n",
		entry.Timestamp.Local().Format("2006-01-02 15:04:05"),
		entry.Pod.Namespace+"/"+entry.Pod.Name,
		strings.ToUpper(entry.Status),
		rootCause,
		entry.Report.Confidence)
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
	if len(report.CollectionErrors) > 0 {
		fmt.Printf("collection errors: %s\n", strings.Join(report.CollectionErrors, "; "))
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
	fmt.Println(`kubewhy explains why a Kubernetes pod is unhealthy

Usage:
  kubewhy diagnose --file request.json [--json] [--explain]
  kubewhy diagnose --file request.json --json --exit-code
  kubewhy diagnose --pod pod/api --namespace payments [--previous]
  kubewhy diagnose --pod pod/api --namespace payments --watch --interval 5s
  kubewhy history [--pod NAME] [--namespace NS] [--since 24h] [--status STATUS] [--last N] [--json]
  kubewhy serve --listen :8080

LLM explanation: set KUBEWHY_LLM_API_KEY (optionally KUBEWHY_LLM_BASE_URL and
KUBEWHY_LLM_MODEL for OpenAI-compatible endpoints such as Ollama), then pass
--explain. In watch mode, explanations run only when the status changes.

Every diagnosis is stored in ~/.kubewhy/history.jsonl; browse it with
"kubewhy history".

POST /api/v1/diagnose with a DiagnoseRequest to use the API.`)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "kubewhy:", err); os.Exit(1) }
