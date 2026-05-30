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
    <div className="space-y-5">
      <section className="panel overflow-hidden">
        <div className="flex items-center justify-between border-b border-border p-4">
          <h2 className="text-sm font-semibold">Registered Models</h2>
          <button className="icon-button" type="button" onClick={() => void refresh()} aria-label="Refresh models" title="Refresh models">
            <RefreshCw className="h-4 w-4" />
          </button>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[720px] border-collapse text-left text-sm">
            <thead className="border-b border-border text-xs uppercase text-muted">
              <tr>
                <th className="px-4 py-3 font-medium">Model Name</th>
                <th className="px-4 py-3 font-medium">VRAM Required</th>
                <th className="px-4 py-3 font-medium">Backend</th>
                <th className="px-4 py-3 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {models.map((model) => (
                <tr key={model.id} className="border-b border-border transition hover:bg-elevated">
                  <td className="px-4 py-3">
                    <div className="font-mono text-primary">{model.id}</div>
                    <div className="mt-1 text-xs text-muted">{model.description}</div>
                  </td>
                  <td className="px-4 py-3">
                    <span className="inline-flex items-center gap-2 font-mono text-primary">
                      <Cpu className="h-4 w-4 text-accent" />
                      {model.vram_gb.toFixed(1)} GB
                    </span>
                  </td>
                  <td className="px-4 py-3 font-mono text-muted">{model.backend}</td>
                  <td className="px-4 py-3">
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
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-4">
          <div>
            <h2 className="text-sm font-semibold">Downloadable Models</h2>
            <div className="mt-1 flex items-center gap-2 text-xs text-muted">
              {catalogOnline ? <Wifi className="h-3.5 w-3.5 text-success" /> : <WifiOff className="h-3.5 w-3.5 text-warn" />}
              <span>{catalogOnline ? 'Ollama library reachable' : 'Waiting for internet access to Ollama library'}</span>
            </div>
          </div>
          <button className="command-button" type="button" onClick={() => void refresh()}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </button>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
          <div className="inline-flex rounded-panel border border-border bg-base p-1">
            <CatalogTab active={catalogCategory === 'standard'} label="Recommended" countLabel="consumer GPUs" onClick={() => void selectCategory('standard')} />
            <CatalogTab active={catalogCategory === 'abliterated'} label="Abliterated" countLabel="separate list" onClick={() => void selectCategory('abliterated')} />
          </div>
          <div className="font-mono text-xs text-muted">
            Showing {catalog.length} of {catalogTotal}
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
            <div className="py-8 text-center text-sm text-muted lg:col-span-2">
              No downloadable model catalog returned.
            </div>
          )}
        </div>
        {catalog.length < catalogTotal && (
          <div className="border-t border-border p-4 text-center">
            <button className="command-button mx-auto" type="button" disabled={loadingMore} onClick={() => void loadMore()}>
              <Download className="h-4 w-4" />
              {loadingMore ? 'Loading' : 'Load More'}
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
      className={`rounded-panel px-3 py-2 text-left transition ${active ? 'bg-elevated text-primary' : 'text-muted hover:text-primary'}`}
      type="button"
      onClick={onClick}
    >
      <span className="block text-xs font-semibold">{label}</span>
      <span className="block text-[10px] uppercase">{countLabel}</span>
    </button>
  );
}

function StatusPill({ model }: { model: ModelInfo }): JSX.Element {
  if (model.status === 'loaded') {
    return <span className="pill border-success bg-elevated text-success">Loaded</span>;
  }
  if (model.status === 'idle') {
    return <span className="pill border-warn bg-elevated text-warn">Idle</span>;
  }
  if (model.status === 'loading') {
    return <span className="pill border-accent bg-[var(--accent-dim)] text-accent">Loading</span>;
  }
  return <span className="pill border-border bg-transparent text-muted">Unloaded</span>;
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
    <article className="rounded-panel border border-border bg-base p-4 transition hover:bg-elevated">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="truncate font-mono text-sm text-primary">{model.id}</h3>
            <CatalogStatus model={model} installed={installed} />
          </div>
          <div className="mt-1 text-sm font-medium">{model.display_name}</div>
          <p className="mt-2 text-sm leading-5 text-muted">{model.description}</p>
        </div>
        <a className="icon-button shrink-0" href={model.library_url} target="_blank" rel="noreferrer" title="Open Ollama library" aria-label="Open Ollama library">
          <ExternalLink className="h-4 w-4" />
        </a>
      </div>

      <div className="mt-4 grid grid-cols-3 gap-2 font-mono text-xs text-muted">
        <Metric label="VRAM" value={`${model.vram_gb.toFixed(1)} GB`} />
        <Metric label="Size" value={`~${model.size_gb.toFixed(1)} GB`} />
        <Metric label="Use" value={model.use_case} />
      </div>

      {(downloading || model.status === 'failed') && (
        <div className="mt-4">
          <div className="mb-2 flex items-center justify-between gap-2 font-mono text-xs text-muted">
            <span className="truncate">{model.message || (downloading ? 'Downloading' : 'Download failed')}</span>
            <span>{Math.max(0, model.progress_pct)}%</span>
          </div>
          <progress className="vram-progress h-2 w-full" max={100} value={Math.min(100, Math.max(0, model.progress_pct))} />
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
        <div className="font-mono text-xs text-muted">{registered ? 'Registered in Aegis' : 'Registers automatically'}</div>
        <button className="command-button" type="button" disabled={disabled} onClick={onDownload}>
          {installed ? <CheckCircle2 className="h-4 w-4" /> : <Download className="h-4 w-4" />}
          {installed ? 'Installed' : downloading ? 'Downloading' : 'Download'}
        </button>
      </div>
    </article>
  );
}

function Metric({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="rounded-panel border border-border bg-surface px-3 py-2">
      <div className="text-[10px] uppercase text-muted">{label}</div>
      <div className="mt-1 truncate text-primary">{value}</div>
    </div>
  );
}

function CatalogStatus({ model, installed }: { model: CatalogModel; installed: boolean }): JSX.Element {
  if (installed) {
    return <span className="pill border-success bg-elevated text-success">Installed</span>;
  }
  if (model.status === 'downloading') {
    return <span className="pill border-accent bg-[var(--accent-dim)] text-accent">Downloading</span>;
  }
  if (model.status === 'failed') {
    return <span className="pill border-danger bg-elevated text-danger">Failed</span>;
  }
  return <span className="pill border-border bg-transparent text-muted">Available</span>;
}
