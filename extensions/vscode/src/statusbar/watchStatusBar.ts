import * as vscode from 'vscode';

export class WatchStatusBar implements vscode.Disposable {
    private item: vscode.StatusBarItem;

    constructor() {
        this.item = vscode.window.createStatusBarItem(
            vscode.StatusBarAlignment.Left,
            50,
        );
        this.item.command = 'kubewhy.watch';
    }

    show(pod: string, namespace: string): void {
        this.item.text = `$(eye) Kubewhy: ${namespace}/${pod}`;
        this.item.tooltip = `Watching ${pod} in ${namespace} — click to stop`;
        this.item.backgroundColor = undefined;
        this.item.show();
    }

    updateStatus(status: string): void {
        switch (status) {
            case 'healthy':
                this.item.backgroundColor = undefined;
                break;
            case 'error':
                this.item.backgroundColor = new vscode.ThemeColor(
                    'statusBarItem.errorBackground',
                );
                break;
            default:
                this.item.backgroundColor = new vscode.ThemeColor(
                    'statusBarItem.warningBackground',
                );
                break;
        }
    }

    hide(): void {
        this.item.hide();
    }

    dispose(): void {
        this.item.dispose();
    }
}
