import { KeyRound, ShieldCheck, TriangleAlert } from 'lucide-react';
import { Component, ErrorInfo, FormEvent, ReactNode, useEffect, useMemo, useState } from 'react';
import toast, { Toaster } from 'react-hot-toast';
import ChatConsole from './components/ChatConsole';
import Compare from './components/Compare';
import Dashboard from './components/Dashboard';
import KeyManager from './components/KeyManager';
import Layout, { ViewKey } from './components/Layout';
import LogTable from './components/LogTable';
import ModelList from './components/ModelList';
import SettingsPanel from './components/SettingsPanel';
import TemplateLibrary from './components/TemplateLibrary';
import { useGatewayStore } from './store/useGatewayStore';

const titles: Record<ViewKey, string> = {
  chat: 'Chat Console',
  compare: 'Compare Models',
  dashboard: 'Dashboard',
  models: 'Models',
  templates: 'Templates',
  logs: 'Audit Log',
  keys: 'API Keys',
  settings: 'Settings',
};

export default function App(): JSX.Element {
  return (
    <AppErrorBoundary>
      <AppContent />
    </AppErrorBoundary>
  );
}

function AppContent(): JSX.Element {
  const [view, setView] = useState<ViewKey>('chat');
  const [inputToken, setInputToken] = useState('');
  const token = useGatewayStore((state) => state.token);
  const connected = useGatewayStore((state) => state.connected);
  const error = useGatewayStore((state) => state.error);
  const config = useGatewayStore((state) => state.config);
  const setToken = useGatewayStore((state) => state.setToken);
  const clearToken = useGatewayStore((state) => state.clearToken);
  const clearCreatedKey = useGatewayStore((state) => state.clearCreatedKey);
  const loadStats = useGatewayStore((state) => state.loadStats);
  const loadHardware = useGatewayStore((state) => state.loadHardware);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const loadConfig = useGatewayStore((state) => state.loadConfig);
  const loadTemplates = useGatewayStore((state) => state.loadTemplates);

  useEffect(() => {
    if (!token) {
      return;
    }
    void loadHardware();
    void loadStats();
    void loadModels();
    void loadConfig();
    void loadTemplates();
  }, [token, loadHardware, loadStats, loadModels, loadConfig, loadTemplates]);

  useEffect(() => {
    if (error) {
      toast.error(error);
    }
  }, [error]);

  const version = useMemo(() => config?.runtime.app_version ?? 'dev', [config]);

  if (!token) {
    const submit = (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      const trimmed = inputToken.trim();
      if (trimmed) {
        setToken(trimmed);
      }
    };

    return (
      <div className="flex min-h-screen items-center justify-center bg-base p-6 text-primary">
        <Toaster position="bottom-right" />
        <div className="w-full max-w-[420px]">
          <div className="mb-1 flex items-center gap-2 text-[10px] uppercase tracking-widest text-muted">
            <ShieldCheck className="h-3 w-3" />
            <span>Aegis Gateway</span>
          </div>
          <div className="border border-border bg-surface">
            <div className="border-b border-border px-5 py-4">
              <div className="text-xs uppercase tracking-widest text-accent">Authentication Required</div>
              <div className="mt-1 text-[10px] uppercase tracking-widest text-muted">Enter a local API key to access mission control</div>
            </div>
            <form className="px-5 py-5" onSubmit={submit}>
              <label className="mb-1.5 block text-[10px] uppercase tracking-widest text-muted" htmlFor="api-key">
                API Key
              </label>
              <div className="flex gap-2">
                <div className="relative flex-1">
                  <div className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-accent">&gt;_</div>
                  <input
                    id="api-key"
                    className="field w-full pl-9"
                    type="password"
                    placeholder="aegis-..."
                    value={inputToken}
                    onChange={(event) => setInputToken(event.target.value)}
                    autoFocus
                  />
                </div>
                <button className="command-button" type="submit">
                  <KeyRound className="h-3.5 w-3.5" />
                  Unlock
                </button>
              </div>
              <div className="mt-4 border border-border bg-elevated px-3 py-2.5">
                <div className="text-[10px] uppercase tracking-widest text-muted">Recovery</div>
                <code className="mt-1 block text-xs text-muted">
                  .\aegis-gateway.exe --reset-admin-key
                </code>
              </div>
            </form>
          </div>
        </div>
      </div>
    );
  }

  const navigate = (next: ViewKey) => {
    clearCreatedKey();
    setView(next);
  };

  return (
    <>
      <Toaster position="bottom-right" />
      <Layout
        activeView={view}
        connected={connected}
        title={titles[view]}
        version={version}
        onNavigate={navigate}
        onSignOut={clearToken}
      >
        {view === 'chat' && <ChatConsole />}
        {view === 'compare' && <Compare />}
        {view === 'dashboard' && <Dashboard />}
        {view === 'models' && <ModelList />}
        {view === 'templates' && <TemplateLibrary />}
        {view === 'logs' && <LogTable />}
        {view === 'keys' && <KeyManager />}
        {view === 'settings' && <SettingsPanel />}
      </Layout>
    </>
  );
}

class AppErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): { error: Error } {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('Aegis dashboard render failed', error, info.componentStack);
  }

  render(): ReactNode {
    if (this.state.error) {
      return (
        <div className="flex min-h-screen items-center justify-center bg-base p-6 text-primary">
          <div className="w-full max-w-[520px] border border-danger bg-surface">
            <div className="border-b border-border px-5 py-3">
              <div className="flex items-center gap-2 text-xs uppercase tracking-widest text-danger">
                <TriangleAlert className="h-3.5 w-3.5" />
                Dashboard Error
              </div>
            </div>
            <div className="px-5 py-4">
              <p className="text-xs leading-6 text-muted">
                The dashboard hit a local browser-state error. Clear the saved key and reload, then paste your current API key again.
              </p>
              <div className="mt-3 border border-border bg-elevated px-3 py-2">
                <code className="block break-all text-xs text-danger">{this.state.error.message}</code>
              </div>
              <button
                className="command-button mt-4"
                type="button"
                onClick={() => {
                  window.localStorage.removeItem('aegis_api_key');
                  window.location.reload();
                }}
              >
                Clear saved key and reload
              </button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}
