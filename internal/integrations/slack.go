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

type SlackNotifier struct {
	WebhookURL string
	HTTPClient *http.Client
}

func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		WebhookURL: webhookURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SlackNotifier) Notify(report model.Report) error {
	statusEmoji := map[string]string{
		"healthy":  ":white_check_mark:",
		"degraded": ":warning:",
		"broken":   ":x:",
		"unknown":  ":question:",
	}
	emoji := statusEmoji[report.Status]
	if emoji == "" {
		emoji = ":grey_question:"
	}

	podName := report.Pod.Name
	if report.Pod.Namespace != "" {
		podName = report.Pod.Namespace + "/" + report.Pod.Name
	}

	header := fmt.Sprintf("%s Pod *%s* is *%s*", emoji, podName, strings.ToUpper(report.Status))

	var blocks []map[string]interface{}
	blocks = append(blocks, map[string]interface{}{
		"type": "header",
		"text": map[string]interface{}{"type": "plain_text", "text": header},
	})

	summary := fmt.Sprintf("*%s* (confidence: %s)\n%s", strings.ToUpper(report.Status), report.Confidence, report.Summary)
	blocks = append(blocks, map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{"type": "mrkdwn", "text": summary},
	})

	if report.RootCause != nil {
		rc := report.RootCause
		rcText := fmt.Sprintf("*Root cause:* %s (%s)\n%s", rc.Title, rc.Code, rc.Explanation)
		if len(rc.Evidence) > 0 {
			rcText += "\n*Evidence:* " + strings.Join(rc.Evidence, "; ")
		}
		if len(rc.Remediation) > 0 {
			rcText += "\n*Next steps:* " + strings.Join(rc.Remediation, "; ")
		}
		if rc.RunbookURL != "" {
			rcText += "\n:book: <" + rc.RunbookURL + "|Runbook>"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]interface{}{"type": "mrkdwn", "text": rcText},
		})
	}

	blocks = append(blocks, map[string]interface{}{
		"type": "context",
		"elements": []map[string]interface{}{
			{"type": "mrkdwn", "text": fmt.Sprintf("Diagnosed at %s by kubewhy", report.GeneratedAt.Format(time.RFC3339))},
		},
	})

	payload := map[string]interface{}{"blocks": blocks}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}

	resp, err := s.HTTPClient.Post(s.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: post webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack: webhook returned status %d", resp.StatusCode)
	}
	return nil
}
