import { Save } from 'lucide-react';
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
    if (!config) {
      return;
    }
    setPort(config.config.server.port);
    setIdleTimeout(config.config.server.idle_timeout_minutes);
    setRateLimit(config.config.security.rate_limit_rpm);
    setOllamaURL(config.config.backend.ollama_base_url);
  }, [config]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!config) {
      return;
    }
    await saveConfig({
      port,
      idle_timeout_minutes: idleTimeout,
      rate_limit_rpm: rateLimit,
      ollama_base_url: ollamaURL,
    });
    toast.success(port !== config.config.server.port ? 'Settings saved. Restart Aegis to use the new port.' : 'Settings saved');
  };

  if (!config) {
    return <section className="panel p-5 text-muted">Loading settings...</section>;
  }

  return (
    <div className="grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
      <section className="panel p-5">
        <h2 className="mb-5 text-sm font-semibold">Editable Config</h2>
        <form className="grid grid-cols-1 gap-4 md:grid-cols-2" onSubmit={submit}>
          <label className="space-y-2">
            <span className="block text-sm text-muted">Port</span>
            <input className="field w-full font-mono" min={1} max={65535} type="number" value={port} onChange={(event) => setPort(Number(event.target.value))} />
            {port !== config.config.server.port && <span className="block text-xs text-warn">Port changes apply after restart.</span>}
          </label>
          <label className="space-y-2">
            <span className="block text-sm text-muted">Idle timeout minutes</span>
            <input className="field w-full font-mono" min={1} type="number" value={idleTimeout} onChange={(event) => setIdleTimeout(Number(event.target.value))} />
          </label>
          <label className="space-y-2">
            <span className="block text-sm text-muted">Rate limit RPM</span>
            <input className="field w-full font-mono" min={0} type="number" value={rateLimit} onChange={(event) => setRateLimit(Number(event.target.value))} />
          </label>
          <label className="space-y-2 md:col-span-2">
            <span className="block text-sm text-muted">Ollama base URL</span>
            <input className="field w-full font-mono" value={ollamaURL} onChange={(event) => setOllamaURL(event.target.value)} />
          </label>
          <div className="md:col-span-2">
            <button className="command-button" type="submit">
              <Save className="h-4 w-4" />
              Save
            </button>
          </div>
        </form>
      </section>

      <aside className="panel p-5">
        <h2 className="mb-5 text-sm font-semibold">Runtime</h2>
        <dl className="space-y-4">
          <RuntimeItem label="Go version" value={config.runtime.go_version} />
          <RuntimeItem label="Uptime" value={formatUptime(config.runtime.uptime_sec)} />
          <RuntimeItem label="Build time" value={config.runtime.build_time} />
          <RuntimeItem label="llama.cpp URL" value={config.config.backend.llamacpp_base_url} />
        </dl>
        {config.restart_required && (
          <div className="mt-5 rounded-panel border border-warn bg-elevated p-3 text-sm text-warn">
            Restart required for the latest saved network settings.
          </div>
        )}
      </aside>
    </div>
  );
}

function RuntimeItem({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div>
      <dt className="text-xs uppercase text-muted">{label}</dt>
      <dd className="mt-1 break-all font-mono text-sm text-primary">{value}</dd>
    </div>
  );
}

function formatUptime(seconds: number): string {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remaining = seconds % 60;
  return `${hours}h ${minutes}m ${remaining}s`;
}
