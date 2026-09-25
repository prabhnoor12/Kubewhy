import * as vscode from 'vscode';
import { readHistory, HistoryEntry } from '../utils/history';

export class HistoryTreeProvider implements vscode.TreeDataProvider<HistoryEntry> {
    private _onDidChangeTreeData = new vscode.EventEmitter<HistoryEntry | undefined | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    getTreeItem(element: HistoryEntry): vscode.TreeItem {
        const label = `${element.pod.namespace}/${element.pod.name}`;
        const item = new vscode.TreeItem(label, vscode.TreeItemCollapsibleState.None);
        item.description = element.status;
        item.tooltip = `${label}\nStatus: ${element.status}\nTime: ${element.timestamp}`;

        const statusIcon = this.statusIcon(element.status);
        item.iconPath = new vscode.ThemeIcon(statusIcon);

        item.contextValue = 'historyEntry';
        item.command = {
            command: 'kubewhy.diagnose',
            title: 'Diagnose',
            arguments: [element.pod.name, element.pod.namespace],
        };

        return item;
    }

    getChildren(): HistoryEntry[] {
        const config = vscode.workspace.getConfiguration('kubewhy');
        const limit = config.get<number>('historyLimit', 50);
        return readHistory(limit);
    }

    private statusIcon(status: string): string {
        switch (status.toLowerCase()) {
            case 'healthy':
            case 'running':
                return 'pass';
            case 'warning':
            case 'degraded':
                return 'warning';
            case 'critical':
            case 'error':
            case 'crashloopbackoff':
                return 'error';
            default:
                return 'question';
        }
    }
}
