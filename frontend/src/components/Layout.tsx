import { BarChart3, Cpu, KeyRound, ListTree, MessageSquare, ScrollText, Settings } from 'lucide-react';
import { ReactNode } from 'react';

export type ViewKey = 'chat' | 'dashboard' | 'models' | 'logs' | 'keys' | 'settings';

interface LayoutProps {
  activeView: ViewKey;
  connected: boolean;
  title: string;
  version: string;
  onNavigate: (view: ViewKey) => void;
  onSignOut: () => void;
  children: ReactNode;
}

const navItems: Array<{
  key: ViewKey;
  label: string;
  icon: typeof BarChart3;
}> = [
  { key: 'chat', label: 'Chat', icon: MessageSquare },
  { key: 'dashboard', label: 'Dashboard', icon: BarChart3 },
  { key: 'models', label: 'Models', icon: ListTree },
  { key: 'logs', label: 'Logs', icon: ScrollText },
  { key: 'keys', label: 'Keys', icon: KeyRound },
  { key: 'settings', label: 'Settings', icon: Settings },
];

export default function Layout({
  activeView,
  connected,
  title,
  version,
  onNavigate,
  onSignOut,
  children,
}: LayoutProps): JSX.Element {
  return (
    <div className="min-h-screen bg-base text-primary">
      <aside className="sticky top-0 z-20 flex w-full flex-col border-b border-border bg-surface md:fixed md:inset-y-0 md:left-0 md:w-[220px] md:border-b-0 md:border-r">
        <div className="flex h-16 shrink-0 items-center gap-3 border-b border-border px-5 md:h-20">
          <div className="flex h-9 w-9 items-center justify-center rounded-panel border border-accent bg-[var(--accent-dim)] text-accent">
            <Cpu className="h-5 w-5" />
          </div>
          <div>
            <div className="text-lg font-bold leading-tight">Aegis</div>
            <div className="font-mono text-xs text-muted">Gateway</div>
          </div>
        </div>

        <nav className="flex gap-1 overflow-x-auto px-3 py-3 md:flex-1 md:flex-col md:space-y-1 md:overflow-visible md:py-4">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = item.key === activeView;
            return (
              <button
                key={item.key}
                className={`flex h-10 shrink-0 items-center gap-3 rounded-panel px-3 text-left text-sm transition md:w-full ${
                  active
                    ? 'bg-[var(--accent-dim)] text-accent'
                    : 'text-muted hover:bg-elevated hover:text-primary'
                }`}
                type="button"
                onClick={() => onNavigate(item.key)}
              >
                <Icon className="h-4 w-4" />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>

        <div className="flex items-center justify-between border-t border-border p-3 md:block md:p-4">
          <button className="text-left text-xs text-muted transition hover:text-primary md:mb-3 md:w-full" type="button" onClick={onSignOut}>
            Sign out
          </button>
          <div className="font-mono text-xs text-muted">v{version}</div>
        </div>
      </aside>

      <main className="min-h-screen bg-base p-4 md:ml-[220px] md:p-6">
        <header className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-bold tracking-normal">{title}</h1>
            <div className="mt-2 flex items-center gap-2 text-sm text-muted">
              <span className={`h-2.5 w-2.5 rounded-full ${connected ? 'animate-pulse bg-success' : 'bg-muted'}`} />
              <span>{connected ? 'Connected' : 'Disconnected'}</span>
            </div>
          </div>
        </header>
        {children}
      </main>
    </div>
  );
}
