import { Cpu, RefreshCw } from 'lucide-react';
import { useEffect } from 'react';
import { ModelInfo } from '../api/client';
import { useGatewayStore } from '../store/useGatewayStore';

export default function ModelList(): JSX.Element {
  const models = useGatewayStore((state) => state.models);
  const loadModels = useGatewayStore((state) => state.loadModels);

  useEffect(() => {
    void loadModels();
    const id = window.setInterval(() => {
      void loadModels();
    }, 10000);
    return () => window.clearInterval(id);
  }, [loadModels]);

  return (
    <section className="panel overflow-hidden">
      <div className="flex items-center justify-between border-b border-border p-4">
        <h2 className="text-sm font-semibold">Registered Models</h2>
        <button className="icon-button" type="button" onClick={() => void loadModels()} aria-label="Refresh models" title="Refresh models">
          <RefreshCw className="h-4 w-4" />
        </button>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[720px] border-collapse text-left text-sm">
          <thead className="border-b border-border text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3 font-medium">Model Name</th>
              <th className="px-4 py-3 font-medium">VRAM Required</th>
              <th className="px-4 py-3 font-medium">Backend</th>
              <th className="px-4 py-3 font-medium">Status</th>
            </tr>
          </thead>
          <tbody>
            {models.map((model) => (
              <tr key={model.id} className="border-b border-border transition hover:bg-elevated">
                <td className="px-4 py-3">
                  <div className="font-mono text-primary">{model.id}</div>
                  <div className="mt-1 text-xs text-muted">{model.description}</div>
                </td>
                <td className="px-4 py-3">
                  <span className="inline-flex items-center gap-2 font-mono text-primary">
                    <Cpu className="h-4 w-4 text-accent" />
                    {model.vram_gb.toFixed(1)} GB
                  </span>
                </td>
                <td className="px-4 py-3 font-mono text-muted">{model.backend}</td>
                <td className="px-4 py-3">
                  <StatusPill model={model} />
                </td>
              </tr>
            ))}
            {models.length === 0 && (
              <tr>
                <td className="px-4 py-8 text-center text-muted" colSpan={4}>
                  No registered models returned.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function StatusPill({ model }: { model: ModelInfo }): JSX.Element {
  if (model.status === 'loaded') {
    return <span className="pill border-success bg-elevated text-success">Loaded</span>;
  }
  if (model.status === 'idle') {
    return <span className="pill border-warn bg-elevated text-warn">Idle</span>;
  }
  if (model.status === 'loading') {
    return <span className="pill border-accent bg-[var(--accent-dim)] text-accent">Loading</span>;
  }
  return <span className="pill border-border bg-transparent text-muted">Unloaded</span>;
}
