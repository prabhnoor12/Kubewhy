import { readFileSync } from 'fs';
import { homedir } from 'os';
import { join } from 'path';

export interface HistoryEntry {
    timestamp: string;
    pod: { name: string; namespace: string };
    status: string;
}

export function historyFilePath(): string {
    return join(homedir(), '.kubewhy', 'history.jsonl');
}

export function readHistory(limit = 50): HistoryEntry[] {
    let raw: string;
    try {
        raw = readFileSync(historyFilePath(), 'utf8');
    } catch {
        return [];
    }
    const entries: HistoryEntry[] = [];
    for (const line of raw.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed) {
            continue;
        }
        try {
            const parsed = JSON.parse(trimmed) as HistoryEntry;
            if (parsed && parsed.pod && parsed.status && parsed.timestamp) {
                entries.push(parsed);
            }
        } catch {
            // Skip malformed lines.
        }
    }
    return entries.slice(-limit).reverse();
}
