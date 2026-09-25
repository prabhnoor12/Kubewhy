import { HistoryTreeProvider } from '../treeview/historyTreeProvider';

export function refreshHistoryCommand(treeProvider: HistoryTreeProvider): void {
    treeProvider.refresh();
}
