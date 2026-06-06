import { BarChart3, Cpu, FileText, GitCompareArrows, KeyRound, ListTree, LogOut, MessageSquare, ScrollText, Settings } from 'lucide-react';
import { ReactNode } from 'react';

export type ViewKey = 'chat' | 'compare' | 'dashboard' | 'models' | 'templates' | 'logs' | 'keys' | 'settings';

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
  { key: 'chat', label: 'Chat Console', icon: MessageSquare },
  { key: 'compare', label: 'Compare', icon: GitCompareArrows },
  { key: 'dashboard', label: 'Dashboard', icon: BarChart3 },
  { key: 'models', label: 'Models', icon: ListTree },
  { key: 'templates', label: 'Templates', icon: FileText },
  { key: 'logs', label: 'Audit Log', icon: ScrollText },
  { key: 'keys', label: 'API Keys', icon: KeyRound },
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
      <aside className="sticky top-0 z-20 flex w-full flex-col border-b border-border bg-surface md:fixed md:inset-y-0 md:left-0 md:w-[200px] md:border-b-0 md:border-r">
        <div className="flex h-14 shrink-0 items-center gap-3 border-b border-border px-4">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center border border-accent bg-[var(--accent-dim)] text-accent">
            <Cpu className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="text-sm font-bold tracking-widest text-accent">AEGIS</div>
            <div className="text-[10px] uppercase tracking-widest text-muted">Gateway</div>
          </div>
        </div>

        <nav className="flex gap-0.5 overflow-x-auto px-2 py-2 md:flex-1 md:flex-col md:overflow-visible md:py-3">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = item.key === activeView;
            return (
              <button
                key={item.key}
                className={`flex h-9 shrink-0 items-center gap-2.5 border-l-2 px-3 text-left text-[11px] uppercase tracking-wider transition md:w-full ${
                  active
                    ? 'border-accent bg-[var(--accent-dim)] text-accent'
                    : 'border-transparent text-muted hover:border-border hover:bg-elevated hover:text-primary'
                }`}
                type="button"
                aria-current={active ? 'page' : undefined}
                onClick={() => onNavigate(item.key)}
              >
                <Icon className="h-3.5 w-3.5 shrink-0" />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>

        <div className="border-t border-border px-4 py-3">
          <button
            className="mb-3 flex w-full items-center gap-2 text-[10px] uppercase tracking-widest text-muted transition hover:text-primary"
            type="button"
            onClick={onSignOut}
          >
            <LogOut className="h-3 w-3" />
            Sign out
          </button>
          <div className="flex items-center gap-2 text-[10px] uppercase tracking-widest text-muted">
            <div className={`h-1.5 w-1.5 shrink-0 ${connected ? 'bg-success' : 'bg-muted'}`} />
            <span>{connected ? 'Online' : 'Offline'}</span>
            <span className="ml-auto">v{version}</span>
          </div>
        </div>
      </aside>

      <main className="min-h-screen bg-base md:ml-[200px]">
        <header className="flex items-center justify-between border-b border-border px-5 py-3">
          <div>
            <div className="text-[10px] uppercase tracking-widest text-muted">Aegis Gateway</div>
            <h1 className="text-sm font-bold uppercase tracking-widest text-primary">{title}</h1>
          </div>
          <div className="flex items-center gap-2 text-[10px] uppercase tracking-widest text-muted">
            <div className={`h-1.5 w-1.5 shrink-0 ${connected ? 'animate-pulse bg-success' : 'bg-muted'}`} />
            <span>{connected ? 'Connected' : 'Disconnected'}</span>
          </div>
        </header>
        <div className="p-4 md:p-5">{children}</div>
      </main>
    </div>
  );
}
