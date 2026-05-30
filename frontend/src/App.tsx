import { KeyRound, ShieldCheck } from 'lucide-react';
import { FormEvent, useEffect, useMemo, useState } from 'react';
import toast, { Toaster } from 'react-hot-toast';
import ChatConsole from './components/ChatConsole';
import Dashboard from './components/Dashboard';
import KeyManager from './components/KeyManager';
import Layout, { ViewKey } from './components/Layout';
import LogTable from './components/LogTable';
import ModelList from './components/ModelList';
import SettingsPanel from './components/SettingsPanel';
import TemplateLibrary from './components/TemplateLibrary';
import { useGatewayStore } from './store/useGatewayStore';

const titles: Record<ViewKey, string> = {
  chat: 'Chat',
  dashboard: 'Dashboard',
  models: 'Models',
  templates: 'Templates',
  logs: 'Audit Logs',
  keys: 'API Keys',
  settings: 'Settings',
};

export default function App(): JSX.Element {
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
        <Toaster position="bottom-right" toastOptions={{ className: 'font-sans' }} />
        <form className="panel w-full max-w-[440px] p-6" onSubmit={submit}>
          <div className="mb-6 flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-panel border border-accent bg-[var(--accent-dim)] text-accent">
              <ShieldCheck className="h-5 w-5" />
            </div>
            <div>
              <h1 className="text-xl font-bold">Aegis Gateway</h1>
              <p className="mt-1 text-sm text-muted">Enter a local API key to open mission control.</p>
            </div>
          </div>
          <label className="mb-2 block text-sm text-muted" htmlFor="api-key">
            API key
          </label>
          <div className="flex gap-2">
            <input
              id="api-key"
              className="field min-w-0 flex-1 font-mono"
              type="password"
              value={inputToken}
              onChange={(event) => setInputToken(event.target.value)}
            />
            <button className="command-button" type="submit">
              <KeyRound className="h-4 w-4" />
              Unlock
            </button>
          </div>
        </form>
      </div>
    );
  }

  const navigate = (next: ViewKey) => {
    clearCreatedKey();
    setView(next);
  };

  return (
    <>
      <Toaster position="bottom-right" toastOptions={{ className: 'font-sans' }} />
      <Layout
        activeView={view}
        connected={connected}
        title={titles[view]}
        version={version}
        onNavigate={navigate}
        onSignOut={clearToken}
      >
        {view === 'chat' && <ChatConsole />}
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
