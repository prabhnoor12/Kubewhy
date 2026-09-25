import * as vscode from 'vscode';
import { DiagnosePanel } from './webview/diagnosePanel';
import { HistoryTreeProvider } from './treeview/historyTreeProvider';
import { WatchStatusBar } from './statusbar/watchStatusBar';
import { diagnoseCommand } from './commands/diagnose';
import { explainCommand } from './commands/explain';
import { watchCommand } from './commands/watch';
import { refreshHistoryCommand } from './commands/refreshHistory';

export function activate(context: vscode.ExtensionContext): void {
    const panel = new DiagnosePanel();
    const treeProvider = new HistoryTreeProvider();
    const statusBar = new WatchStatusBar();

    context.subscriptions.push(
        panel,
        statusBar,
        vscode.window.registerTreeDataProvider('kubewhy.history', treeProvider),
    );

    context.subscriptions.push(
        vscode.commands.registerCommand('kubewhy.diagnose', (pod?: string, namespace?: string) =>
            diagnoseCommand(panel, pod, namespace),
        ),
        vscode.commands.registerCommand('kubewhy.explain', (pod?: string, namespace?: string) =>
            explainCommand(panel, pod, namespace),
        ),
        vscode.commands.registerCommand('kubewhy.watch', () =>
            watchCommand(statusBar),
        ),
        vscode.commands.registerCommand('kubewhy.refreshHistory', () =>
            refreshHistoryCommand(treeProvider),
        ),
    );
}

export function deactivate(): void {
    // All disposables are tracked via context.subscriptions.
}
