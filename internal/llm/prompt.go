package llm

import (
	"fmt"
	"strings"

	"github.com/kubewhy/kubewhy/internal/model"
)

const systemPrompt = `You are a Kubernetes debugging assistant. The user ran kubewhy to diagnose an unhealthy pod, and kubewhy produced a structured diagnosis.

Your task:
1. Explain the diagnosis in plain, conversational language for an engineer who knows Kubernetes basics.
2. Highlight the most urgent action first.
3. Provide specific, copy-paste-ready kubectl commands for each remediation step when possible.
4. If multiple issues exist, explain how they relate to each other (root cause vs symptoms).
5. Be concise. Do not repeat the raw diagnosis verbatim; interpret it.`

// BuildPrompt constructs the chat messages from a diagnosis report.
func BuildPrompt(report model.Report) []Message {
	return []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: buildUserContent(report)},
	}
}

const simpleSystemPrompt = `You are a Kubernetes debugging assistant. The user is new to Kubernetes and needs a very simple explanation.

Your task:
1. Explain what is wrong in 2-3 sentences using plain language a beginner would understand.
2. Avoid jargon. If you must use a technical term, explain it briefly.
3. Give exactly one action the user should take, as a simple step.
4. Do not include kubectl commands unless absolutely necessary, and if you do, explain what each part means.`

func BuildPromptSimple(report model.Report) []Message {
	return []Message{
		{Role: "system", Content: simpleSystemPrompt},
		{Role: "user", Content: buildUserContent(report)},
	}
}

func buildUserContent(report model.Report) string {
	var b strings.Builder

	b.WriteString("Diagnosis for pod ")
	b.WriteString(podIdentity(report.Pod))
	b.WriteString(":\n\n")

	fmt.Fprintf(&b, "Status: %s (confidence: %s)\n", report.Status, report.Confidence)
	if report.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", report.Summary)
	}
	if len(report.MissingContext) > 0 {
		fmt.Fprintf(&b, "Missing context: %s (treat conclusions as less certain)\n", strings.Join(report.MissingContext, ", "))
	}
	if len(report.CollectionErrors) > 0 {
		fmt.Fprintf(&b, "Collection errors: %s\n", strings.Join(report.CollectionErrors, "; "))
	}

	if report.RootCause != nil {
		b.WriteString("\nRoot cause:\n")
		writeReason(&b, *report.RootCause)
	}

	if len(report.Reasons) > 0 {
		b.WriteString("\nAll findings (ranked):\n")
		for i, reason := range report.Reasons {
			fmt.Fprintf(&b, "%d. ", i+1)
			writeReason(&b, reason)
		}
	}

	if len(report.Containers) > 0 {
		b.WriteString("\nContainer states:\n")
		for _, c := range report.Containers {
			fmt.Fprintf(&b, "- %s (%s): state=%s ready=%t restarts=%d", c.Name, c.Kind, c.State, c.Ready, c.RestartCount)
			if len(c.Details) > 0 {
				fmt.Fprintf(&b, " — %s", strings.Join(c.Details, "; "))
			}
			b.WriteString("\n")
		}
	}

	if len(report.RelevantEvents) > 0 {
		b.WriteString("\nRelevant Kubernetes events:\n")
		for _, e := range report.RelevantEvents {
			fmt.Fprintf(&b, "- [%s] %s (count=%d): %s\n", e.Severity, e.Reason, e.Count, e.Message)
		}
	}

	if len(report.ResourceFindings) > 0 {
		b.WriteString("\nResource findings:\n")
		for _, r := range report.ResourceFindings {
			fmt.Fprintf(&b, "- [%s] %s: %s", r.Severity, r.Title, r.Explanation)
			if len(r.Evidence) > 0 {
				fmt.Fprintf(&b, " (evidence: %s)", strings.Join(r.Evidence, "; "))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\nExplain what is happening and what I should do next.")
	return b.String()
}

func writeReason(b *strings.Builder, reason model.Reason) {
	fmt.Fprintf(b, "[%s/%s] %s (%s)\n", strings.ToUpper(reason.Severity), reason.Confidence, reason.Title, reason.Code)
	if reason.Explanation != "" {
		fmt.Fprintf(b, "   %s\n", reason.Explanation)
	}
	if len(reason.Evidence) > 0 {
		fmt.Fprintf(b, "   evidence: %s\n", strings.Join(reason.Evidence, "; "))
	}
	if len(reason.Remediation) > 0 {
		fmt.Fprintf(b, "   suggested remediation: %s\n", strings.Join(reason.Remediation, "; "))
	}
}

func podIdentity(pod model.PodIdentity) string {
	if pod.Namespace != "" {
		return pod.Namespace + "/" + pod.Name
	}
	return pod.Name
}
