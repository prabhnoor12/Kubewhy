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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kubewhy/kubewhy/internal/api"
	"github.com/kubewhy/kubewhy/internal/collector"
	"github.com/kubewhy/kubewhy/internal/diagnosis"
	"github.com/kubewhy/kubewhy/internal/history"
	"github.com/kubewhy/kubewhy/internal/integrations"
	"github.com/kubewhy/kubewhy/internal/llm"
	"github.com/kubewhy/kubewhy/internal/model"
	"github.com/kubewhy/kubewhy/internal/runbook"
)

func Run(args []string) {
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "diagnose":
		runDiagnose(args[1:])
	case "diagnose-namespace":
		runDiagnoseNamespace(args[1:])
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
	runbooksPath := flags.String("runbooks", "", "path to runbook mapping JSON file")
	simple := flags.Bool("simple", false, "show a plain-language explanation for beginners")
	slackWebhook := flags.String("slack-webhook", "", "Slack webhook URL for notifications (or KUBEWHY_SLACK_WEBHOOK)")
	pagerdutyKey := flags.String("pagerduty-key", "", "PagerDuty routing key for alerts (or KUBEWHY_PAGERDUTY_KEY)")
	_ = flags.Parse(args)

	notifiers := buildNotifiers(*slackWebhook, *pagerdutyKey)

	engine := diagnosis.NewEngine()
	if *runbooksPath != "" {
		mapping, err := runbook.LoadMapping(*runbooksPath)
		if err != nil {
			fatal(err)
		}
		engine.Runbooks = mapping
	}

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
		code := runClusterDiagnose(ctx, engine, cluster, *namespace, podName, collector.Options{TailLines: *tailLines, PreviousLogs: *previousLogs}, *asJSON, *watch, *interval, *timeout, explainConfig{enabled: *explain, url: *llmURL, key: *llmKey, model: *llmModel})
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
	report := engine.Diagnose(request)
	writeHistory(report)
	attachPatternMemory(&report)
	if *simple {
		fmt.Println(simpleExplanation(report))
		if *explain {
			explainReport(report, explainConfig{enabled: true, url: *llmURL, key: *llmKey, model: *llmModel, simple: true})
		}
		notifyAll(notifiers, report)
		if *exitCodes {
			os.Exit(reportExitCode(report))
		}
		return
	}
	if *asJSON {
		writeIndented(report)
	} else {
		printReport(report)
	}
	if *explain {
		explainReport(report, explainConfig{enabled: true, url: *llmURL, key: *llmKey, model: *llmModel})
	}
	notifyAll(notifiers, report)
	if *exitCodes {
		os.Exit(reportExitCode(report))
	}
}

func runDiagnoseNamespace(args []string) {
	flags := flag.NewFlagSet("diagnose-namespace", flag.ExitOnError)
	namespace := flags.String("namespace", "default", "Kubernetes namespace")
	asJSON := flags.Bool("json", false, "write the workload report as JSON")
	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig")
	kubeContext := flags.String("context", "", "kubeconfig context")
	tailLines := flags.Int64("tail", 200, "log lines per container")
	previousLogs := flags.Bool("previous", false, "collect previous container logs")
	exitCodes := flags.Bool("exit-code", false, "exit non-zero when any pod is unhealthy")
	_ = flags.Parse(args)

	cluster, err := collector.NewFromKubeconfig(*kubeconfig, *kubeContext)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	requests, err := cluster.CollectNamespace(ctx, *namespace, collector.Options{TailLines: *tailLines, PreviousLogs: *previousLogs})
	if err != nil {
		fatal(err)
	}

	engine := diagnosis.NewEngine()
	workload := model.WorkloadReport{
		GeneratedAt: time.Now().UTC(),
		Namespace:   *namespace,
		Reports:     []model.Report{},
	}

	allPods, _ := cluster.ListPodCount(ctx, *namespace)
	workload.TotalPods = allPods

	causeCounts := map[string]string{}
	for _, req := range requests {
		report := engine.Diagnose(req)
		writeHistory(report)
		workload.Reports = append(workload.Reports, report)
		if report.Status != "healthy" {
			workload.UnhealthyCount++
		} else {
			workload.HealthyCount++
		}
		if report.RootCause != nil {
			causeCounts[report.RootCause.Code] = report.RootCause.Title
		}
	}
	workload.HealthyCount = workload.TotalPods - workload.UnhealthyCount

	type causeEntry struct {
		code  string
		title string
		count int
	}
	var causes []causeEntry
	seen := map[string]int{}
	for _, r := range workload.Reports {
		if r.RootCause != nil {
			seen[r.RootCause.Code]++
		}
	}
	for code, count := range seen {
		causes = append(causes, causeEntry{code: code, title: causeCounts[code], count: count})
	}
	sort.Slice(causes, func(i, j int) bool { return causes[i].count > causes[j].count })
	for _, c := range causes {
		if len(workload.CommonCauses) >= 5 {
			break
		}
		workload.CommonCauses = append(workload.CommonCauses, model.CauseCount{Code: c.code, Title: c.title, Count: c.count})
	}

	if *asJSON {
		writeIndented(workload)
	} else {
		printWorkloadReport(workload)
	}
	if *exitCodes && workload.UnhealthyCount > 0 {
		os.Exit(2)
	}
}

func printWorkloadReport(w model.WorkloadReport) {
	fmt.Printf("Namespace: %s\n", w.Namespace)
	fmt.Printf("Pods: %d total, %d healthy, %d unhealthy\n\n", w.TotalPods, w.HealthyCount, w.UnhealthyCount)
	if len(w.CommonCauses) > 0 {
		fmt.Println("Common root causes:")
		for _, c := range w.CommonCauses {
			fmt.Printf("  - %s (%s): %d pod(s)\n", c.Title, c.Code, c.Count)
		}
	}
	for _, report := range w.Reports {
		fmt.Printf("\n--- %s/%s: %s ---\n", report.Pod.Namespace, report.Pod.Name, strings.ToUpper(report.Status))
		if report.RootCause != nil {
			fmt.Printf("  Root cause: %s (%s)\n", report.RootCause.Title, report.RootCause.Code)
		}
	}
}

type explainConfig struct {
	enabled bool
	url     string
	key     string
	model   string
	simple  bool
}

func runClusterDiagnose(ctx context.Context, engine *diagnosis.Engine, cluster *collector.Collector, namespace, podName string, options collector.Options, asJSON, watch bool, interval, timeout time.Duration, explain explainConfig) int {
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
		request, err := cluster.CollectWithDeployment(collectCtx, namespace, podName, options)
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
			report := engine.Diagnose(request)
			lastStatus = report.Status
			attachPatternMemory(&report)
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
	messages := llm.BuildPrompt(report)
	if cfg.simple {
		messages = llm.BuildPromptSimple(report)
	}
	explanation, err := llm.NewClient(config).Chat(ctx, messages)
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
	if len(report.PreviousOccurrences) > 0 {
		fmt.Printf("\nPattern memory: %d previous occurrence(s) of %s in the last 7 days\n", len(report.PreviousOccurrences), report.PreviousOccurrences[0].RootCauseCode)
		for _, occ := range report.PreviousOccurrences {
			fmt.Printf("  - %s: %s\n", occ.Timestamp.Format("2006-01-02 15:04"), strings.ToUpper(occ.Status))
		}
	}
	if report.DeploymentContext != nil {
		dc := report.DeploymentContext
		fmt.Printf("\nDeployment: %s/%s\n", dc.Namespace, dc.Name)
		if dc.ActiveRollout {
			fmt.Printf("  Active rollout (revision %s)\n", dc.CurrentRevision)
		} else if dc.CurrentRevision != "" {
			fmt.Printf("  Current revision: %s\n", dc.CurrentRevision)
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

func buildNotifiers(slackWebhook, pagerdutyKey string) []integrations.Notifier {
	if slackWebhook == "" {
		slackWebhook = os.Getenv("KUBEWHY_SLACK_WEBHOOK")
	}
	if pagerdutyKey == "" {
		pagerdutyKey = os.Getenv("KUBEWHY_PAGERDUTY_KEY")
	}
	var notifiers []integrations.Notifier
	if slackWebhook != "" {
		notifiers = append(notifiers, integrations.NewSlackNotifier(slackWebhook))
	}
	if pagerdutyKey != "" {
		notifiers = append(notifiers, integrations.NewPagerDutyNotifier(pagerdutyKey))
	}
	return notifiers
}

func notifyAll(notifiers []integrations.Notifier, report model.Report) {
	for _, n := range notifiers {
		if err := n.Notify(report); err != nil {
			fmt.Fprintf(os.Stderr, "kubewhy: notification failed: %v\n", err)
		}
	}
}

func simpleExplanation(report model.Report) string {
	var statusMsg string
	switch report.Status {
	case "healthy":
		statusMsg = "Your pod is running fine."
	case "degraded":
		statusMsg = "Your pod is running but something is not quite right."
	case "broken":
		statusMsg = "Your pod is not working properly."
	default:
		statusMsg = "We could not figure out what is going on with your pod."
	}
	if report.RootCause == nil {
		return statusMsg
	}
	var causeMsg string
	switch report.RootCause.Code {
	case "oom_killed":
		causeMsg = "It used too much memory and was stopped by Kubernetes."
	case "crash_loop":
		causeMsg = "It keeps crashing and restarting over and over."
	case "image_pull_backoff", "image_pull":
		causeMsg = "Kubernetes cannot download the container image."
	case "log_dependency_unavailable":
		causeMsg = "It cannot connect to a service it depends on."
	case "log_config_error":
		causeMsg = "Its configuration is missing something it needs to start."
	case "pod_failed":
		causeMsg = "It failed to run."
	case "container_exit":
		causeMsg = "The program inside the container stopped with an error."
	case "restart_rapid":
		causeMsg = "It crashes almost immediately after starting."
	case "restart_creep":
		causeMsg = "It runs for a while, then slowly runs out of memory and dies."
	case "restart_after_stable":
		causeMsg = "It was working fine for a long time, then something changed and it stopped."
	case "resource_no_feasible_node":
		causeMsg = "There is no server in your cluster with enough resources to run it."
	default:
		causeMsg = report.RootCause.Explanation
	}
	result := statusMsg + " " + causeMsg
	if len(report.RootCause.Remediation) > 0 {
		result += " Try: " + report.RootCause.Remediation[0] + "."
	}
	return result
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "kubewhy:", err); os.Exit(1) }

func attachPatternMemory(report *model.Report) {
	if report.RootCause == nil {
		return
	}
	if historyStore == nil {
		historyOnce.Do(func() {
			store, err := history.NewStore("")
			if err != nil {
				return
			}
			historyStore = store
		})
	}
	if historyStore == nil {
		return
	}
	occurrences, err := historyStore.QueryByPattern(report.Pod.Name, report.Pod.Namespace, report.RootCause.Code, time.Now().Add(-7*24*time.Hour))
	if err != nil || len(occurrences) == 0 {
		return
	}
	for _, occ := range occurrences {
		report.PreviousOccurrences = append(report.PreviousOccurrences, model.PreviousOccurrence{
			Timestamp:     occ.Timestamp,
			Status:        occ.Status,
			RootCauseCode: occ.Report.RootCause.Code,
		})
	}
}
