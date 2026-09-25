import * as vscode from 'vscode';
import { ReportSummary } from '../utils/cli';

export class DiagnosePanel implements vscode.Disposable {
    private panel: vscode.WebviewPanel | undefined;
    private disposables: vscode.Disposable[] = [];

    show(report: ReportSummary): void {
        if (!this.panel) {
            this.panel = vscode.window.createWebviewPanel(
                'kubewhyDiagnose',
                'Kubewhy Diagnosis',
                vscode.ViewColumn.One,
                {
                    enableScripts: false,
                    retainContextWhenHidden: true,
                },
            );
            this.panel.onDidDispose(() => {
                this.panel = undefined;
            }, null, this.disposables);
        }

        this.panel.title = this.panelTitle(report);
        this.panel.webview.html = this.renderHtml(report);
        this.panel.reveal(vscode.ViewColumn.One, false);
    }

    private panelTitle(report: ReportSummary): string {
        const pod = report.pod?.name ?? 'unknown';
        const ns = report.pod?.namespace ?? '';
        return ns ? `Kubewhy: ${ns}/${pod}` : `Kubewhy: ${pod}`;
    }

    private renderHtml(report: ReportSummary): string {
        const statusColor = this.statusColor(report.status);
        const rootCauseHtml = report.rootCause
            ? `<div class="root-cause">
                <h2>Root Cause</h2>
                <div class="badge">${this.esc(report.rootCause.code)}</div>
                <h3>${this.esc(report.rootCause.title)}</h3>
                ${report.rootCause.explanation ? `<p>${this.esc(report.rootCause.explanation)}</p>` : ''}
               </div>`
            : '';

        const reasonsHtml = report.reasons
            .map(
                (r) => `
            <div class="reason severity-${this.esc(r.severity)}">
                <div class="reason-header">
                    <span class="badge">${this.esc(r.code)}</span>
                    <span class="severity">${this.esc(r.severity)}</span>
                    <span class="confidence">${this.esc(r.confidence)}</span>
                </div>
                <h3>${this.esc(r.title)}</h3>
                <p>${this.esc(r.explanation)}</p>
                ${r.evidence.length ? `<h4>Evidence</h4><ul>${r.evidence.map((e) => `<li>${this.esc(e)}</li>`).join('')}</ul>` : ''}
                ${r.remediation.length ? `<h4>Remediation</h4><ol>${r.remediation.map((s) => `<li>${this.esc(s)}</li>`).join('')}</ol>` : ''}
            </div>`,
            )
            .join('\n');

        const containersHtml = report.containers?.length
            ? `<h2>Containers</h2>
               <table>
                 <tr><th>Name</th><th>State</th><th>Ready</th><th>Restarts</th></tr>
                 ${report.containers.map((c) => `<tr>
                    <td>${this.esc(c.name)}</td>
                    <td>${this.esc(c.state ?? '-')}</td>
                    <td>${c.ready ? 'Yes' : 'No'}</td>
                    <td>${c.restartCount ?? 0}</td>
                 </tr>`).join('')}
               </table>`
            : '';

        const eventsHtml = report.relevantEvents?.length
            ? `<h2>Relevant Events</h2>
               <ul>${report.relevantEvents.map((e) => `<li><strong>${this.esc(e.reason)}</strong>: ${this.esc(e.message)}${e.count ? ` (x${e.count})` : ''}</li>`).join('')}</ul>`
            : '';

        const errorsHtml = report.collectionErrors?.length
            ? `<div class="errors"><h2>Collection Errors</h2><ul>${report.collectionErrors.map((e) => `<li>${this.esc(e)}</li>`).join('')}</ul></div>`
            : '';

        return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<style>
  body { font-family: var(--vscode-font-family); padding: 16px; color: var(--vscode-foreground); background: var(--vscode-editor-background); }
  h1 { margin-bottom: 4px; }
  h2 { margin-top: 24px; border-bottom: 1px solid var(--vscode-panel-border); padding-bottom: 4px; }
  h3 { margin: 4px 0; }
  h4 { margin: 8px 0 4px; }
  .header { display: flex; align-items: center; gap: 12px; }
  .status { display: inline-block; padding: 2px 10px; border-radius: 4px; font-weight: bold; color: #fff; background: ${statusColor}; }
  .confidence { color: var(--vscode-descriptionForeground); font-size: 0.9em; }
  .badge { display: inline-block; padding: 1px 8px; border-radius: 3px; background: var(--vscode-badge-background); color: var(--vscode-badge-foreground); font-size: 0.85em; margin-right: 6px; }
  .root-cause { border-left: 4px solid var(--vscode-terminal-ansiRed); padding: 8px 16px; margin: 16px 0; background: var(--vscode-textBlockQuote-background); }
  .reason { margin: 16px 0; padding: 12px; border: 1px solid var(--vscode-panel-border); border-radius: 4px; }
  .reason-header { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .severity { font-weight: bold; text-transform: uppercase; font-size: 0.8em; }
  .severity-critical { border-left: 4px solid var(--vscode-terminal-ansiRed); }
  .severity-warning { border-left: 4px solid var(--vscode-terminal-ansiYellow); }
  .severity-info { border-left: 4px solid var(--vscode-terminal-ansiBlue); }
  table { border-collapse: collapse; width: 100%; margin: 8px 0; }
  th, td { text-align: left; padding: 6px 12px; border: 1px solid var(--vscode-panel-border); }
  th { background: var(--vscode-editor-inactiveSelectionBackground); }
  ul, ol { padding-left: 24px; }
  .errors { border: 1px solid var(--vscode-terminal-ansiYellow); padding: 12px; border-radius: 4px; margin-top: 16px; }
  .summary { font-size: 1.05em; margin: 12px 0; }
</style>
</head>
<body>
  <div class="header">
    <h1>${this.esc(report.pod?.name ?? 'Unknown Pod')}</h1>
    <span class="status">${this.esc(report.status)}</span>
    <span class="confidence">Confidence: ${this.esc(report.confidence)}</span>
  </div>
  <p class="summary">${this.esc(report.summary)}</p>
  ${rootCauseHtml}
  ${reasonsHtml ? `<h2>Reasons</h2>${reasonsHtml}` : ''}
  ${containersHtml}
  ${eventsHtml}
  ${errorsHtml}
</body>
</html>`;
    }

    private statusColor(status: string): string {
        switch (status.toLowerCase()) {
            case 'healthy':
            case 'running':
                return '#388a38';
            case 'warning':
            case 'degraded':
                return '#c4a000';
            default:
                return '#cd3131';
        }
    }

    private esc(s: string): string {
        return s
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;');
    }

    dispose(): void {
        this.panel?.dispose();
        for (const d of this.disposables) {
            d.dispose();
        }
    }
}
