import * as vscode from 'vscode';
import { explain as runExplain, KubewhyError, ReportSummary } from '../utils/cli';
import { DiagnosePanel } from '../webview/diagnosePanel';

export async function explainCommand(
    panel: DiagnosePanel,
    pod?: string,
    namespace?: string,
): Promise<ReportSummary | undefined> {
    const config = vscode.workspace.getConfiguration('kubewhy');
    const defaultNs = config.get<string>('defaultNamespace', 'default');

    if (!pod) {
        pod = await vscode.window.showInputBox({
            prompt: 'Pod name',
            placeHolder: 'my-pod',
            validateInput: (v) => (v.trim() ? undefined : 'Pod name is required'),
        });
    }
    if (!pod) {
        return undefined;
    }

    if (namespace === undefined) {
        namespace = await vscode.window.showInputBox({
            prompt: 'Namespace',
            value: defaultNs,
            placeHolder: 'default',
        });
    }
    if (!namespace) {
        return undefined;
    }

    return vscode.window.withProgress(
        {
            location: vscode.ProgressLocation.Notification,
            title: `Explaining ${pod} with LLM…`,
            cancellable: false,
        },
        async () => {
            try {
                const report = await runExplain(pod!, namespace!);
                panel.show(report);
                return report;
            } catch (err) {
                const msg = err instanceof KubewhyError ? err.message : String(err);
                vscode.window.showErrorMessage(`Kubewhy explain failed: ${msg}`);
                return undefined;
            }
        },
    );
}
