import { spawn } from 'child_process';
import * as vscode from 'vscode';

export class KubewhyError extends Error {
    constructor(message: string) {
        super(message);
        this.name = 'KubewhyError';
    }
}

export function cliPath(): string {
    const config = vscode.workspace.getConfiguration('kubewhy');
    return config.get<string>('cliPath', 'kubewhy');
}

export function runKubewhy(args: string[], timeoutMs = 60000): Promise<string> {
    return new Promise((resolve, reject) => {
        const config = vscode.workspace.getConfiguration('kubewhy');
        const context = config.get<string>('context', '');
        const allArgs: string[] = [];
        if (context) {
            allArgs.push('--context', context);
        }
        allArgs.push(...args);
        const child = spawn(cliPath(), allArgs, { shell: process.platform === 'win32' });
        let stdout = '';
        let stderr = '';
        const timer = setTimeout(() => {
            child.kill();
            reject(new KubewhyError(`kubewhy timed out after ${timeoutMs / 1000}s`));
        }, timeoutMs);

        child.stdout.on('data', (data: Buffer) => { stdout += data.toString(); });
        child.stderr.on('data', (data: Buffer) => { stderr += data.toString(); });
        child.on('error', (err) => {
            clearTimeout(timer);
            reject(new KubewhyError(
                `Could not run "${cliPath()}". Install it with "go install github.com/kubewhy/kubewhy/cmd/kubewhy@latest" and make sure it is on PATH. (${err.message})`
            ));
        });
        child.on('close', (code) => {
            clearTimeout(timer);
            if (code === 0 || stdout.trim().length > 0) {
                resolve(stdout);
            } else {
                reject(new KubewhyError(stderr.trim() || `kubewhy exited with code ${code}`));
            }
        });
    });
}

export async function runKubewhyJSON<T>(args: string[]): Promise<T> {
    const output = await runKubewhy([...args, '--json']);
    return JSON.parse(output) as T;
}

export interface ReportSummary {
    pod: { name?: string; namespace?: string; node?: string; phase?: string };
    status: string;
    confidence: string;
    summary: string;
    rootCause?: { code: string; title: string; explanation?: string } | null;
    reasons: Array<{
        code: string;
        severity: string;
        confidence: string;
        title: string;
        explanation: string;
        evidence: string[];
        remediation: string[];
    }>;
    containers?: Array<{ name: string; kind?: string; state?: string; ready?: boolean; restartCount?: number; details?: string[] }>;
    relevantEvents?: Array<{ reason: string; message: string; count?: number; severity?: string }>;
    missingContext?: string[];
    collectionErrors?: string[];
}

export async function diagnose(pod: string, namespace: string): Promise<ReportSummary> {
    return runKubewhyJSON<ReportSummary>(['diagnose', '--pod', pod, '--namespace', namespace]);
}

export async function explain(pod: string, namespace: string): Promise<ReportSummary> {
    return runKubewhyJSON<ReportSummary>(['diagnose', '--pod', pod, '--namespace', namespace, '--explain']);
}
