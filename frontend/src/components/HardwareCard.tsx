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

  return (
    <section className="panel p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-panel border border-border bg-elevated text-accent">
            <Cpu className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-sm text-muted">Hardware</div>
            <div className="truncate font-mono text-lg font-semibold">
              {detected ? hardware?.gpu_name : 'No NVIDIA GPU detected'}
            </div>
          </div>
        </div>
        {!detected && (
          <span className="pill border-warn bg-elevated text-warn">
            <TriangleAlert className="h-3.5 w-3.5" />
            CPU Only Mode
          </span>
        )}
        {detected && hardware?.temperature_c !== undefined && (
          <span className="pill border-border bg-elevated text-muted">
            <Thermometer className="h-3.5 w-3.5" />
            <span className="font-mono">{hardware.temperature_c}C</span>
          </span>
        )}
      </div>

      <div className="space-y-2">
        <progress className="vram-progress h-2 w-full" max={Math.max(total, 1)} value={used} />
        <div className="flex items-center justify-between font-mono text-xs text-muted">
          <span>{detected ? `${used.toFixed(1)} GB used` : '0.0 GB used'}</span>
          <span>{detected ? `${total.toFixed(1)} GB total` : '0.0 GB total'}</span>
        </div>
      </div>
    </section>
  );
}
