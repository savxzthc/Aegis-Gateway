import { Cpu, Thermometer, TriangleAlert } from 'lucide-react';
import { useEffect } from 'react';
import { useGatewayStore } from '../store/useGatewayStore';

export default function HardwareCard(): JSX.Element {
  const hardware = useGatewayStore((state) => state.hardware);
  const loadHardware = useGatewayStore((state) => state.loadHardware);

  useEffect(() => {
    void loadHardware();
    const id = window.setInterval(() => {
      void loadHardware();
    }, 5000);
    return () => window.clearInterval(id);
  }, [loadHardware]);

  const total = hardware?.vram_total_gb ?? 0;
  const used = hardware?.vram_used_gb ?? 0;
  const detected = hardware?.detected ?? false;
  const pct = total > 0 ? (used / total) * 100 : 0;

  return (
    <section className="panel p-4">
      <div className="mb-3 flex items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center border border-border bg-elevated text-accent">
            <Cpu className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-widest text-muted">Hardware</div>
            <div className="truncate text-sm font-bold text-primary">
              {detected ? hardware?.gpu_name : 'No NVIDIA GPU detected'}
            </div>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {!detected && (
            <span className="pill border-warn text-warn">
              <TriangleAlert className="h-3 w-3" />
              CPU Only
            </span>
          )}
          {detected && hardware?.temperature_c !== undefined && (
            <span className="pill border-border text-muted">
              <Thermometer className="h-3 w-3" />
              {hardware.temperature_c}°C
            </span>
          )}
        </div>
      </div>

      <div className="space-y-1.5">
        <progress className="vram-progress h-1.5 w-full" max={Math.max(total, 1)} value={used} />
        <div className="flex items-center justify-between text-[10px] uppercase tracking-wider text-muted">
          <span>
            VRAM {detected ? `${used.toFixed(1)} / ${total.toFixed(1)} GB` : '0.0 / 0.0 GB'}
          </span>
          <span className={pct > 90 ? 'text-danger' : pct > 70 ? 'text-warn' : 'text-muted'}>
            {pct.toFixed(0)}%
          </span>
        </div>
      </div>
    </section>
  );
}
