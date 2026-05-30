import { Activity, Gauge, GitBranch, TrendingDown, TrendingUp, Zap } from 'lucide-react';
import { useEffect } from 'react';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { useGatewayStore } from '../store/useGatewayStore';
import HardwareCard from './HardwareCard';

export default function Dashboard(): JSX.Element {
  const stats = useGatewayStore((state) => state.stats);
  const hardware = useGatewayStore((state) => state.hardware);
  const loadStats = useGatewayStore((state) => state.loadStats);

  useEffect(() => {
    void loadStats();
    const id = window.setInterval(() => {
      void loadStats();
    }, 10000);
    return () => window.clearInterval(id);
  }, [loadStats]);

  const today = stats?.requests_today ?? 0;
  const yesterday = stats?.requests_yesterday ?? 0;
  const trendUp = today >= yesterday;
  const avgLatency = stats?.avg_latency_ms ?? 0;
  const activeModel = stats?.active_model || 'None loaded';
  const chartData =
    stats?.requests_per_hour.map((item) => ({
      hour: new Date(item.hour).toLocaleTimeString([], { hour: '2-digit' }),
      count: item.count,
    })) ?? [];

  return (
    <div className="space-y-5">
      <HardwareCard />

      <section className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <StatCard
          icon={<Activity className="h-5 w-5" />}
          label="Requests Today"
          value={today.toLocaleString()}
          detail={`${trendUp ? 'Up' : 'Down'} vs ${yesterday.toLocaleString()} yesterday`}
          tone={trendUp ? 'success' : 'muted'}
          detailIcon={trendUp ? <TrendingUp className="h-4 w-4" /> : <TrendingDown className="h-4 w-4" />}
        />
        <StatCard
          icon={<Gauge className="h-5 w-5" />}
          label="Avg Latency"
          value={`${avgLatency} ms`}
          detail={latencyLabel(avgLatency)}
          tone={latencyTone(avgLatency)}
        />
        <div className="panel p-5">
          <div className="mb-4 flex items-center gap-3 text-muted">
            <Zap className="h-5 w-5 text-accent" />
            <span className="text-sm">Active Model</span>
          </div>
          <div className="mb-4 truncate font-mono text-3xl font-semibold">{activeModel}</div>
          <progress
            className="vram-progress h-2 w-full"
            max={Math.max(hardware?.vram_total_gb ?? 1, 1)}
            value={hardware?.vram_used_gb ?? 0}
          />
        </div>
        <StatCard
          icon={<GitBranch className="h-5 w-5" />}
          label="Fallback Rate"
          value={`${(stats?.fallback_rate_pct ?? 0).toFixed(1)}%`}
          detail="Last recorded traffic"
          tone={(stats?.fallback_rate_pct ?? 0) > 10 ? 'warn' : 'success'}
        />
      </section>

      <section className="panel p-5">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-sm font-semibold text-primary">Requests per Hour</h2>
          <span className="font-mono text-xs text-muted">last 24h</span>
        </div>
        <div className="h-[280px]">
          <ResponsiveContainer height="100%" width="100%">
            <BarChart data={chartData}>
              <CartesianGrid stroke="var(--border)" vertical={false} />
              <XAxis dataKey="hour" stroke="var(--text-muted)" tickLine={false} axisLine={false} />
              <YAxis stroke="var(--text-muted)" tickLine={false} axisLine={false} allowDecimals={false} />
              <Tooltip cursor={false} content={<ChartTooltip />} />
              <Bar dataKey="count" fill="var(--accent)" radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </div>
      </section>
    </div>
  );
}

function ChartTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: Array<{ value?: number | string }>;
  label?: string;
}): JSX.Element | null {
  if (!active || !payload || payload.length === 0) {
    return null;
  }
  return (
    <div className="rounded-panel border border-border bg-elevated px-3 py-2 text-sm shadow-xl">
      <div className="font-mono text-muted">{label}</div>
      <div className="font-mono text-accent">{payload[0].value ?? 0} requests</div>
    </div>
  );
}

interface StatCardProps {
  icon: JSX.Element;
  label: string;
  value: string;
  detail: string;
  tone: 'success' | 'warn' | 'danger' | 'muted';
  detailIcon?: JSX.Element;
}

function StatCard({ icon, label, value, detail, tone, detailIcon }: StatCardProps): JSX.Element {
  return (
    <div className="panel p-5">
      <div className="mb-4 flex items-center gap-3 text-muted">
        <span className="text-accent">{icon}</span>
        <span className="text-sm">{label}</span>
      </div>
      <div className={`mb-3 font-mono text-3xl font-semibold ${toneClass(tone)}`}>{value}</div>
      <div className="flex items-center gap-2 text-sm text-muted">
        {detailIcon}
        <span>{detail}</span>
      </div>
    </div>
  );
}

function latencyTone(value: number): 'success' | 'warn' | 'danger' {
  if (value < 200) {
    return 'success';
  }
  if (value <= 500) {
    return 'warn';
  }
  return 'danger';
}

function latencyLabel(value: number): string {
  if (value < 200) {
    return 'fast';
  }
  if (value <= 500) {
    return 'moderate';
  }
  return 'slow';
}

function toneClass(tone: StatCardProps['tone']): string {
  if (tone === 'success') {
    return 'text-success';
  }
  if (tone === 'warn') {
    return 'text-warn';
  }
  if (tone === 'danger') {
    return 'text-danger';
  }
  return 'text-primary';
}
