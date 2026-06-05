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
  const activeModel = stats?.active_model || 'none';
  const chartData =
    stats?.requests_per_hour.map((item) => ({
      hour: new Date(item.hour).toLocaleTimeString([], { hour: '2-digit' }),
      count: item.count,
    })) ?? [];

  return (
    <div className="space-y-4">
      <HardwareCard />

      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <StatCard
          icon={<Activity className="h-4 w-4" />}
          label="Req Today"
          value={today.toLocaleString()}
          detail={`${yesterday.toLocaleString()} yesterday`}
          tone={trendUp ? 'success' : 'muted'}
          trendIcon={trendUp ? <TrendingUp className="h-3.5 w-3.5" /> : <TrendingDown className="h-3.5 w-3.5" />}
        />
        <StatCard
          icon={<Gauge className="h-4 w-4" />}
          label="Avg Latency"
          value={`${avgLatency} ms`}
          detail={latencyLabel(avgLatency)}
          tone={latencyTone(avgLatency)}
        />
        <StatCard
          icon={<GitBranch className="h-4 w-4" />}
          label="Fallback Rate"
          value={`${(stats?.fallback_rate_pct ?? 0).toFixed(1)}%`}
          detail="of traffic"
          tone={(stats?.fallback_rate_pct ?? 0) > 10 ? 'warn' : 'success'}
        />
        <div className="panel p-4">
          <div className="mb-1 flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted">
            <Zap className="h-3.5 w-3.5 text-accent" />
            Active Model
          </div>
          <div className="truncate text-sm font-bold text-primary">{activeModel}</div>
          <progress
            className="vram-progress mt-3 h-1 w-full"
            max={Math.max(hardware?.vram_total_gb ?? 1, 1)}
            value={hardware?.vram_used_gb ?? 0}
          />
        </div>
      </div>

      <section className="panel">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-muted">Requests / Hour</div>
          <div className="text-[10px] uppercase tracking-widest text-muted">Last 24h</div>
        </div>
        <div className="h-[240px] px-2 py-3">
          <ResponsiveContainer height="100%" width="100%">
            <BarChart data={chartData} barCategoryGap="30%">
              <CartesianGrid stroke="var(--border)" vertical={false} />
              <XAxis
                dataKey="hour"
                stroke="var(--text-muted)"
                tickLine={false}
                axisLine={false}
                tick={{ fontSize: 10 }}
              />
              <YAxis
                stroke="var(--text-muted)"
                tickLine={false}
                axisLine={false}
                allowDecimals={false}
                tick={{ fontSize: 10 }}
                width={28}
              />
              <Tooltip cursor={{ fill: 'var(--bg-elevated)' }} content={<ChartTooltip />} />
              <Bar dataKey="count" fill="var(--accent)" radius={0} />
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
    <div className="border border-border bg-elevated px-3 py-2 text-xs">
      <div className="text-muted">{label}</div>
      <div className="text-accent">{payload[0].value ?? 0} req</div>
    </div>
  );
}

interface StatCardProps {
  icon: JSX.Element;
  label: string;
  value: string;
  detail: string;
  tone: 'success' | 'warn' | 'danger' | 'muted';
  trendIcon?: JSX.Element;
}

function StatCard({ icon, label, value, detail, tone, trendIcon }: StatCardProps): JSX.Element {
  return (
    <div className="panel p-4">
      <div className="mb-1 flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted">
        <span className="text-accent">{icon}</span>
        {label}
      </div>
      <div className={`text-2xl font-bold ${toneClass(tone)}`}>{value}</div>
      <div className="mt-1 flex items-center gap-1 text-[10px] text-muted">
        {trendIcon}
        <span>{detail}</span>
      </div>
    </div>
  );
}

function latencyTone(value: number): 'success' | 'warn' | 'danger' {
  if (value < 200) return 'success';
  if (value <= 500) return 'warn';
  return 'danger';
}

function latencyLabel(value: number): string {
  if (value < 200) return 'fast';
  if (value <= 500) return 'moderate';
  return 'slow';
}

function toneClass(tone: StatCardProps['tone']): string {
  if (tone === 'success') return 'text-success';
  if (tone === 'warn') return 'text-warn';
  if (tone === 'danger') return 'text-danger';
  return 'text-primary';
}
