import * as vscode from 'vscode';
import { runKubewhy, KubewhyError } from '../utils/cli';
import { WatchStatusBar } from '../statusbar/watchStatusBar';

export class WatchSession implements vscode.Disposable {
    private timer: ReturnType<typeof setInterval> | undefined;
    private pod: string;
    private namespace: string;
    private intervalMs: number;
    private statusBar: WatchStatusBar;
    private outputChannel: vscode.OutputChannel;
    private disposables: vscode.Disposable[] = [];

    constructor(
        pod: string,
        namespace: string,
        statusBar: WatchStatusBar,
        intervalMs = 30000,
    ) {
        this.pod = pod;
        this.namespace = namespace;
        this.statusBar = statusBar;
        this.intervalMs = intervalMs;
        this.outputChannel = vscode.window.createOutputChannel('Kubewhy Watch');
        this.disposables.push(this.outputChannel);
    }

    start(): void {
        this.outputChannel.show(true);
        this.outputChannel.appendLine(
            `[kubewhy] Watching ${this.pod} in ${this.namespace} every ${this.intervalMs / 1000}s`,
        );
        this.statusBar.show(this.pod, this.namespace);
        this.tick();
        this.timer = setInterval(() => this.tick(), this.intervalMs);
    }

    private async tick(): Promise<void> {
        try {
            const output = await runKubewhy(
                ['diagnose', '--pod', this.pod, '--namespace', this.namespace],
                this.intervalMs - 5000,
            );
            const ts = new Date().toISOString();
            this.outputChannel.appendLine(`\n--- ${ts} ---`);
            this.outputChannel.appendLine(output);
            this.statusBar.updateStatus('healthy');
        } catch (err) {
            const msg = err instanceof KubewhyError ? err.message : String(err);
            this.outputChannel.appendLine(`[error] ${msg}`);
            this.statusBar.updateStatus('error');
        }
    }

    dispose(): void {
        if (this.timer) {
            clearInterval(this.timer);
            this.timer = undefined;
        }
        this.statusBar.hide();
        for (const d of this.disposables) {
            d.dispose();
        }
        this.disposables = [];
    }
}

let activeSession: WatchSession | undefined;

export async function watchCommand(statusBar: WatchStatusBar): Promise<void> {
    if (activeSession) {
        activeSession.dispose();
        activeSession = undefined;
        vscode.window.showInformationMessage('Kubewhy: watch stopped');
        return;
    }

    const config = vscode.workspace.getConfiguration('kubewhy');
    const defaultNs = config.get<string>('defaultNamespace', 'default');

    const pod = await vscode.window.showInputBox({
        prompt: 'Pod name to watch',
        placeHolder: 'my-pod',
        validateInput: (v) => (v.trim() ? undefined : 'Pod name is required'),
    });
    if (!pod) {
        return;
    }

    const namespace = await vscode.window.showInputBox({
        prompt: 'Namespace',
        value: defaultNs,
        placeHolder: 'default',
    });
    if (!namespace) {
        return;
    }

    const intervalStr = await vscode.window.showInputBox({
        prompt: 'Poll interval in seconds',
        value: '30',
        placeHolder: '30',
        validateInput: (v) => {
            const n = Number(v);
            return Number.isFinite(n) && n >= 5 ? undefined : 'Must be at least 5 seconds';
        },
    });
    if (!intervalStr) {
        return;
    }

    activeSession = new WatchSession(pod, namespace, statusBar, Number(intervalStr) * 1000);
    activeSession.start();
}
