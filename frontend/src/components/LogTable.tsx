import { ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react';
import { useEffect } from 'react';
import { useGatewayStore } from '../store/useGatewayStore';

export default function LogTable(): JSX.Element {
  const logs = useGatewayStore((state) => state.logs);
  const limit = useGatewayStore((state) => state.logLimit);
  const offset = useGatewayStore((state) => state.logOffset);
  const total = useGatewayStore((state) => state.logTotal);
  const loadLogs = useGatewayStore((state) => state.loadLogs);

  useEffect(() => {
    void loadLogs(offset);
  }, [loadLogs, offset]);

  const previous = Math.max(offset - limit, 0);
  const next = offset + limit;

  return (
    <section className="panel overflow-hidden">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <div className="text-[10px] uppercase tracking-widest text-muted">Request Metadata</div>
        <div className="flex items-center gap-2">
          <button className="command-button" type="button" disabled={offset === 0} onClick={() => void loadLogs(previous)}>
            <ChevronLeft className="h-3.5 w-3.5" />
            Prev
          </button>
          <button className="command-button" type="button" disabled={next >= total} onClick={() => void loadLogs(next)}>
            Next
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>
      <div className="border-b border-border px-4 py-2 text-[10px] uppercase tracking-wider text-muted">
        {logs.length === 0 ? '0' : `${offset + 1}-${Math.min(offset + logs.length, total)}`} of {total}
      </div>

      <div className="overflow-x-auto">
        <table className="w-full min-w-[860px] border-collapse text-left text-xs">
          <thead className="border-b border-border">
            <tr className="text-[10px] uppercase tracking-wider text-muted">
              <th className="px-4 py-2.5 font-medium">Timestamp</th>
              <th className="px-4 py-2.5 font-medium">Requested</th>
              <th className="px-4 py-2.5 font-medium">Used</th>
              <th className="px-4 py-2.5 font-medium">Latency</th>
              <th className="px-4 py-2.5 font-medium">Tokens</th>
              <th className="px-4 py-2.5 font-medium">Status</th>
            </tr>
          </thead>
          <tbody>
            {logs.map((log) => (
              <tr key={log.id} className="border-b border-border transition hover:bg-elevated">
                <td className="px-4 py-2.5 text-muted" title={new Date(log.timestamp).toLocaleString()}>
                  {relativeTime(log.timestamp)}
                </td>
                <td className="px-4 py-2.5 text-primary">{log.model_requested || '-'}</td>
                <td className={`px-4 py-2.5 ${log.model_used !== log.model_requested ? 'text-warn' : 'text-primary'}`}>
                  <span className="inline-flex items-center gap-1.5">
                    {log.model_used !== log.model_requested && <RotateCcw className="h-3 w-3" />}
                    {log.model_used || '-'}
                  </span>
                </td>
                <td className={`px-4 py-2.5 ${latencyClass(log.latency_ms)}`}>{log.latency_ms} ms</td>
                <td className="px-4 py-2.5 text-muted">
                  {log.estimated_prompt_tokens + log.estimated_completion_tokens}
                </td>
                <td className={`px-4 py-2.5 font-bold ${log.status_code >= 400 ? 'text-danger' : 'text-success'}`}>
                  {log.status_code}
                </td>
              </tr>
            ))}
            {logs.length === 0 && (
              <tr>
                <td className="px-4 py-8 text-center text-muted" colSpan={6}>
                  No request metadata yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function latencyClass(value: number): string {
  if (value < 200) return 'text-success';
  if (value <= 500) return 'text-warn';
  return 'text-danger';
}

function relativeTime(value: string): string {
  const then = new Date(value).getTime();
  const diffSeconds = Math.max(1, Math.floor((Date.now() - then) / 1000));
  if (diffSeconds < 60) return `${diffSeconds}s ago`;
  const minutes = Math.floor(diffSeconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
