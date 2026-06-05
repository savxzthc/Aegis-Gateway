import { CheckCircle2, Cpu, Download, ExternalLink, RefreshCw, Wifi, WifiOff } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import toast from 'react-hot-toast';
import { CatalogCategory, CatalogModel, GatewayAPIError, ModelInfo, gatewayErrorMessage } from '../api/client';
import { useGatewayStore } from '../store/useGatewayStore';

export default function ModelList(): JSX.Element {
  const models = useGatewayStore((state) => state.models);
  const catalog = useGatewayStore((state) => state.modelCatalog);
  const catalogOnline = useGatewayStore((state) => state.catalogOnline);
  const catalogCategory = useGatewayStore((state) => state.catalogCategory);
  const catalogTotal = useGatewayStore((state) => state.catalogTotal);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const loadModelCatalog = useGatewayStore((state) => state.loadModelCatalog);
  const pullCatalogModel = useGatewayStore((state) => state.pullCatalogModel);
  const [pulling, setPulling] = useState('');
  const [loadingMore, setLoadingMore] = useState(false);
  const catalogCategoryRef = useRef(catalogCategory);
  const catalogCountRef = useRef(catalog.length);

  useEffect(() => {
    catalogCategoryRef.current = catalogCategory;
  }, [catalogCategory]);

  useEffect(() => {
    catalogCountRef.current = catalog.length;
  }, [catalog.length]);

  useEffect(() => {
    void loadModels();
    void loadModelCatalog(catalogCategoryRef.current);
    const id = window.setInterval(() => {
      void loadModels();
      void loadModelCatalog(catalogCategoryRef.current, false, Math.max(12, catalogCountRef.current));
    }, 5000);
    return () => window.clearInterval(id);
  }, [loadModelCatalog, loadModels]);

  const registered = useMemo(() => new Set(models.map((model) => model.id)), [models]);

  const refresh = async () => {
    await Promise.all([loadModels(), loadModelCatalog(catalogCategory, false, Math.max(12, catalog.length))]);
  };

  const selectCategory = async (category: CatalogCategory) => {
    await loadModelCatalog(category);
  };

  const loadMore = async () => {
    setLoadingMore(true);
    try {
      await loadModelCatalog(catalogCategory, true);
    } finally {
      setLoadingMore(false);
    }
  };

  const startDownload = async (model: CatalogModel) => {
    setPulling(model.id);
    try {
      await pullCatalogModel(model.id);
      toast.success(`${model.display_name} download started`);
    } catch (error) {
      toast.error(error instanceof GatewayAPIError ? gatewayErrorMessage(error) : 'Download could not start');
    } finally {
      setPulling('');
    }
  };

  return (
    <div className="space-y-4">
      <section className="panel overflow-hidden">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-muted">Registered Models</div>
          <button className="icon-button" type="button" onClick={() => void refresh()} aria-label="Refresh models" title="Refresh">
            <RefreshCw className="h-3.5 w-3.5" />
          </button>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] border-collapse text-left text-xs">
            <thead className="border-b border-border">
              <tr className="text-[10px] uppercase tracking-wider text-muted">
                <th className="px-4 py-2.5 font-medium">Model Name</th>
                <th className="px-4 py-2.5 font-medium">VRAM</th>
                <th className="px-4 py-2.5 font-medium">Backend</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {models.map((model) => (
                <tr key={model.id} className="border-b border-border transition hover:bg-elevated">
                  <td className="px-4 py-2.5">
                    <div className="text-primary">{model.id}</div>
                    <div className="mt-0.5 text-[10px] text-muted">{model.description}</div>
                  </td>
                  <td className="px-4 py-2.5">
                    <span className="inline-flex items-center gap-1.5 text-primary">
                      <Cpu className="h-3.5 w-3.5 text-accent" />
                      {model.vram_gb.toFixed(1)} GB
                    </span>
                  </td>
                  <td className="px-4 py-2.5 text-muted">{model.backend}</td>
                  <td className="px-4 py-2.5">
                    <StatusPill model={model} />
                  </td>
                </tr>
              ))}
              {models.length === 0 && (
                <tr>
                  <td className="px-4 py-8 text-center text-muted" colSpan={4}>
                    No registered models returned.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

      <section className="panel overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
          <div>
            <div className="text-[10px] uppercase tracking-widest text-muted">Downloadable Models</div>
            <div className="mt-1 flex items-center gap-2 text-[10px] text-muted">
              {catalogOnline ? (
                <Wifi className="h-3 w-3 text-success" />
              ) : (
                <WifiOff className="h-3 w-3 text-warn" />
              )}
              <span>{catalogOnline ? 'Ollama library reachable' : 'Waiting for internet access'}</span>
            </div>
          </div>
          <button className="command-button" type="button" onClick={() => void refresh()}>
            <RefreshCw className="h-3.5 w-3.5" />
            Refresh
          </button>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-2.5">
          <div className="flex border border-border bg-base">
            <CatalogTab
              active={catalogCategory === 'standard'}
              label="Recommended"
              countLabel="consumer GPUs"
              onClick={() => void selectCategory('standard')}
            />
            <CatalogTab
              active={catalogCategory === 'abliterated'}
              label="Abliterated"
              countLabel="separate list"
              onClick={() => void selectCategory('abliterated')}
            />
          </div>
          <div className="text-[10px] uppercase tracking-wider text-muted">
            {catalog.length} / {catalogTotal}
          </div>
        </div>
        <div className="grid gap-3 p-4 lg:grid-cols-2">
          {catalog.map((model) => (
            <CatalogCard
              key={model.id}
              model={model}
              registered={registered.has(model.id) || model.registered}
              online={catalogOnline}
              busy={pulling === model.id}
              onDownload={() => void startDownload(model)}
            />
          ))}
          {catalog.length === 0 && (
            <div className="py-8 text-center text-xs text-muted lg:col-span-2">
              No downloadable model catalog returned.
            </div>
          )}
        </div>
        {catalog.length < catalogTotal && (
          <div className="border-t border-border p-4 text-center">
            <button className="command-button mx-auto" type="button" disabled={loadingMore} onClick={() => void loadMore()}>
              <Download className="h-3.5 w-3.5" />
              {loadingMore ? 'Loading...' : 'Load More'}
            </button>
          </div>
        )}
      </section>
    </div>
  );
}

function CatalogTab({
  active,
  label,
  countLabel,
  onClick,
}: {
  active: boolean;
  label: string;
  countLabel: string;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      className={`border-r border-border px-3 py-2 text-left transition last:border-r-0 ${
        active ? 'bg-elevated text-primary' : 'text-muted hover:text-primary'
      }`}
      type="button"
      onClick={onClick}
    >
      <span className="block text-[11px] font-semibold uppercase tracking-wider">{label}</span>
      <span className="block text-[9px] uppercase tracking-wider text-muted">{countLabel}</span>
    </button>
  );
}

function StatusPill({ model }: { model: ModelInfo }): JSX.Element {
  if (model.status === 'loaded') return <span className="pill border-success text-success">Loaded</span>;
  if (model.status === 'idle') return <span className="pill border-warn text-warn">Idle</span>;
  if (model.status === 'loading') return <span className="pill border-accent text-accent">Loading</span>;
  return <span className="pill border-border text-muted">Unloaded</span>;
}

function CatalogCard({
  model,
  registered,
  online,
  busy,
  onDownload,
}: {
  model: CatalogModel;
  registered: boolean;
  online: boolean;
  busy: boolean;
  onDownload: () => void;
}): JSX.Element {
  const downloading = model.status === 'downloading';
  const installed = model.installed || model.status === 'installed';
  const disabled = !online || downloading || busy || installed;

  return (
    <article className="border border-border bg-base p-4 transition hover:bg-elevated">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate text-xs font-bold text-primary">{model.id}</h3>
            <CatalogStatus model={model} installed={installed} />
          </div>
          <div className="mt-0.5 text-xs font-medium text-muted">{model.display_name}</div>
          <p className="mt-2 text-xs leading-5 text-muted">{model.description}</p>
        </div>
        <a
          className="icon-button shrink-0"
          href={model.library_url}
          target="_blank"
          rel="noreferrer"
          title="Open Ollama library"
          aria-label="Open Ollama library"
        >
          <ExternalLink className="h-3.5 w-3.5" />
        </a>
      </div>

      <div className="mt-3 grid grid-cols-3 gap-2 text-[10px]">
        <Metric label="VRAM" value={`${model.vram_gb.toFixed(1)} GB`} />
        <Metric label="Size" value={`~${model.size_gb.toFixed(1)} GB`} />
        <Metric label="Use" value={model.use_case} />
      </div>

      {(downloading || model.status === 'failed') && (
        <div className="mt-3">
          <div className="mb-1.5 flex items-center justify-between gap-2 text-[10px] text-muted">
            <span className="truncate">{model.message || (downloading ? 'Downloading' : 'Download failed')}</span>
            <span>{Math.max(0, model.progress_pct)}%</span>
          </div>
          <progress className="vram-progress h-1 w-full" max={100} value={Math.min(100, Math.max(0, model.progress_pct))} />
        </div>
      )}

      <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
        <div className="text-[10px] text-muted">{registered ? 'Registered in Aegis' : 'Auto-registers on install'}</div>
        <button className="command-button" type="button" disabled={disabled} onClick={onDownload}>
          {installed ? <CheckCircle2 className="h-3.5 w-3.5" /> : <Download className="h-3.5 w-3.5" />}
          {installed ? 'Installed' : downloading ? 'Downloading' : 'Download'}
        </button>
      </div>
    </article>
  );
}

function Metric({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="border border-border bg-surface px-2.5 py-2">
      <div className="text-[9px] uppercase tracking-wider text-muted">{label}</div>
      <div className="mt-0.5 truncate text-xs text-primary">{value}</div>
    </div>
  );
}

function CatalogStatus({ model, installed }: { model: CatalogModel; installed: boolean }): JSX.Element {
  if (installed) return <span className="pill border-success text-success">Installed</span>;
  if (model.status === 'downloading') return <span className="pill border-accent text-accent">Downloading</span>;
  if (model.status === 'failed') return <span className="pill border-danger text-danger">Failed</span>;
  return <span className="pill border-border text-muted">Available</span>;
}
