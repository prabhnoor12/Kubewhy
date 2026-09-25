package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

const pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

type PagerDutyNotifier struct {
	RoutingKey string
	EventsURL  string
	HTTPClient *http.Client
}

func NewPagerDutyNotifier(routingKey string) *PagerDutyNotifier {
	return &PagerDutyNotifier{
		RoutingKey: routingKey,
		EventsURL:  pagerDutyEventsURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *PagerDutyNotifier) Notify(report model.Report) error {
	podName := report.Pod.Name
	if report.Pod.Namespace != "" {
		podName = report.Pod.Namespace + "/" + report.Pod.Name
	}

	summary := fmt.Sprintf("Pod %s is %s", podName, report.Status)
	if report.RootCause != nil {
		summary += ": " + report.RootCause.Title
	}

	severity := mapStatusToSeverity(report.Status)

	detail := map[string]interface{}{
		"status":     report.Status,
		"confidence": report.Confidence,
		"summary":    report.Summary,
	}
	if report.RootCause != nil {
		detail["root_cause_code"] = report.RootCause.Code
		detail["root_cause_title"] = report.RootCause.Title
		detail["root_cause_explanation"] = report.RootCause.Explanation
		if len(report.RootCause.Evidence) > 0 {
			detail["evidence"] = strings.Join(report.RootCause.Evidence, "; ")
		}
		if len(report.RootCause.Remediation) > 0 {
			detail["remediation"] = strings.Join(report.RootCause.Remediation, "; ")
		}
	}

	payload := map[string]interface{}{
		"routing_key":  p.RoutingKey,
		"event_action": "trigger",
		"payload": map[string]interface{}{
			"summary":   summary,
			"severity":  severity,
			"source":    "kubewhy",
			"component": podName,
			"group":     report.Pod.Namespace,
			"timestamp": report.GeneratedAt.UTC().Format(time.RFC3339),
			"custom_details": detail,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pagerduty: marshal payload: %w", err)
	}

	resp, err := p.HTTPClient.Post(p.EventsURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty: post event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty: events API returned status %d", resp.StatusCode)
	}
	return nil
}

func mapStatusToSeverity(status string) string {
	switch status {
	case "broken":
		return "critical"
	case "degraded":
		return "warning"
	case "unknown":
		return "info"
	default:
		return "info"
	}
}
