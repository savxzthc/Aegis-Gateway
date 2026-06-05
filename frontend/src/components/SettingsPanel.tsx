import { Save, TriangleAlert } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { useGatewayStore } from '../store/useGatewayStore';

export default function SettingsPanel(): JSX.Element {
  const config = useGatewayStore((state) => state.config);
  const loadConfig = useGatewayStore((state) => state.loadConfig);
  const saveConfig = useGatewayStore((state) => state.saveConfig);
  const [port, setPort] = useState(9000);
  const [idleTimeout, setIdleTimeout] = useState(10);
  const [rateLimit, setRateLimit] = useState(60);
  const [ollamaURL, setOllamaURL] = useState('');

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  useEffect(() => {
    if (!config) return;
    setPort(config.config.server.port);
    setIdleTimeout(config.config.server.idle_timeout_minutes);
    setRateLimit(config.config.security.rate_limit_rpm);
    setOllamaURL(config.config.backend.ollama_base_url);
  }, [config]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!config) return;
    await saveConfig({
      port,
      idle_timeout_minutes: idleTimeout,
      rate_limit_rpm: rateLimit,
      ollama_base_url: ollamaURL,
    });
    toast.success(
      port !== config.config.server.port ? 'Settings saved. Restart Aegis to use the new port.' : 'Settings saved',
    );
  };

  if (!config) {
    return (
      <section className="panel px-4 py-3 text-xs text-muted">Loading config...</section>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
      <section className="panel">
        <div className="border-b border-border px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-muted">Editable Config</div>
        </div>
        <form className="grid grid-cols-1 gap-3 p-4 md:grid-cols-2" onSubmit={submit}>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted" htmlFor="cfg-port">
              Port
            </label>
            <input
              id="cfg-port"
              className="field w-full"
              min={1}
              max={65535}
              type="number"
              value={port}
              onChange={(event) => setPort(Number(event.target.value))}
            />
            {port !== config.config.server.port && (
              <div className="flex items-center gap-1.5 text-[10px] text-warn">
                <TriangleAlert className="h-3 w-3" />
                Port changes apply after restart.
              </div>
            )}
          </div>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted" htmlFor="cfg-idle">
              Idle Timeout (minutes)
            </label>
            <input
              id="cfg-idle"
              className="field w-full"
              min={1}
              type="number"
              value={idleTimeout}
              onChange={(event) => setIdleTimeout(Number(event.target.value))}
            />
          </div>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted" htmlFor="cfg-rpm">
              Rate Limit (RPM)
            </label>
            <input
              id="cfg-rpm"
              className="field w-full"
              min={0}
              type="number"
              value={rateLimit}
              onChange={(event) => setRateLimit(Number(event.target.value))}
            />
          </div>
          <div className="space-y-1.5 md:col-span-2">
            <label className="block text-[10px] uppercase tracking-wider text-muted" htmlFor="cfg-ollama">
              Ollama Base URL
            </label>
            <input
              id="cfg-ollama"
              className="field w-full"
              value={ollamaURL}
              onChange={(event) => setOllamaURL(event.target.value)}
            />
          </div>
          <div className="md:col-span-2">
            <button className="command-button" type="submit">
              <Save className="h-3.5 w-3.5" />
              Save Config
            </button>
          </div>
        </form>
      </section>

      <aside className="panel">
        <div className="border-b border-border px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-muted">Runtime</div>
        </div>
        <dl className="divide-y divide-border">
          <RuntimeItem label="Go Version" value={config.runtime.go_version} />
          <RuntimeItem label="Uptime" value={formatUptime(config.runtime.uptime_sec)} />
          <RuntimeItem label="Build Time" value={config.runtime.build_time} />
          <RuntimeItem label="llama.cpp URL" value={config.config.backend.llamacpp_base_url} />
        </dl>
        {config.restart_required && (
          <div className="m-4 flex items-start gap-2 border border-warn bg-elevated p-3 text-xs text-warn">
            <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            Restart required for network settings to take effect.
          </div>
        )}
      </aside>
    </div>
  );
}

function RuntimeItem({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="px-4 py-3">
      <dt className="text-[9px] uppercase tracking-widest text-muted">{label}</dt>
      <dd className="mt-0.5 break-all text-xs text-primary">{value}</dd>
    </div>
  );
}

function formatUptime(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remaining = seconds % 60;
  return `${hours}h ${minutes}m ${remaining}s`;
}
