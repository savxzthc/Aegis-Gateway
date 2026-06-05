import { Check, Copy, KeyRound, Plus, ShieldCheck, Trash2 } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { useGatewayStore } from '../store/useGatewayStore';

export default function KeyManager(): JSX.Element {
  const [label, setLabel] = useState('');
  const [allowedModels, setAllowedModels] = useState<string[]>([]);
  const [copied, setCopied] = useState(false);
  const keys = useGatewayStore((state) => state.keys);
  const models = useGatewayStore((state) => state.models);
  const createdKey = useGatewayStore((state) => state.createdKey);
  const loadKeys = useGatewayStore((state) => state.loadKeys);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const createAPIKey = useGatewayStore((state) => state.createAPIKey);
  const revokeAPIKey = useGatewayStore((state) => state.revokeAPIKey);
  const saveKeyModels = useGatewayStore((state) => state.saveKeyModels);

  useEffect(() => {
    void loadKeys();
    void loadModels();
  }, [loadKeys, loadModels]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    await createAPIKey(label, allowedModels);
    setLabel('');
    setAllowedModels([]);
    setCopied(false);
  };

  const copyKey = async () => {
    if (!createdKey) return;
    await navigator.clipboard.writeText(createdKey.key);
    setCopied(true);
    toast.success('Key copied');
  };

  return (
    <div className="space-y-4">
      <section className="panel">
        <div className="flex items-center gap-3 border-b border-border px-4 py-3">
          <KeyRound className="h-4 w-4 text-accent" />
          <div className="text-[10px] uppercase tracking-widest text-muted">Create New Key</div>
        </div>
        <div className="p-4">
          <form className="flex flex-col gap-3 sm:flex-row" onSubmit={submit}>
            <input
              className="field min-w-0 flex-1"
              placeholder="Label"
              value={label}
              onChange={(event) => setLabel(event.target.value)}
            />
            <button className="command-button justify-center" type="submit">
              <Plus className="h-3.5 w-3.5" />
              Create
            </button>
          </form>
          <ModelACLPicker
            className="mt-4"
            models={models.map((model) => model.id)}
            selected={allowedModels}
            onChange={setAllowedModels}
          />
          {createdKey && (
            <div className="mt-4 border border-accent bg-[var(--accent-dim)] p-4">
              <div className="mb-2 text-[10px] uppercase tracking-widest text-accent">New key — {createdKey.label}</div>
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
                <code className="min-w-0 flex-1 break-all border border-border bg-base p-3 text-xs text-accent">
                  {createdKey.key}
                </code>
                <button className="command-button justify-center" type="button" onClick={() => void copyKey()}>
                  {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                  {copied ? 'Copied' : 'Copy'}
                </button>
              </div>
            </div>
          )}
        </div>
      </section>

      <section className="panel overflow-hidden">
        <div className="border-b border-border px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-muted">Active API Keys</div>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] border-collapse text-left text-xs">
            <thead className="border-b border-border">
              <tr className="text-[10px] uppercase tracking-wider text-muted">
                <th className="px-4 py-2.5 font-medium">ID</th>
                <th className="px-4 py-2.5 font-medium">Label</th>
                <th className="px-4 py-2.5 font-medium">Created</th>
                <th className="px-4 py-2.5 font-medium">Last Used</th>
                <th className="px-4 py-2.5 font-medium">Requests</th>
                <th className="px-4 py-2.5 font-medium">Model Access</th>
                <th className="px-4 py-2.5 font-medium">Action</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((key) => (
                <tr key={key.id} className="border-b border-border transition hover:bg-elevated">
                  <td className="px-4 py-2.5 text-muted">{key.id}</td>
                  <td className="px-4 py-2.5 text-primary">{key.label}</td>
                  <td className="px-4 py-2.5 text-muted">{new Date(key.created_at).toLocaleString()}</td>
                  <td className="px-4 py-2.5 text-muted">
                    {key.last_used ? new Date(key.last_used).toLocaleString() : 'Never'}
                  </td>
                  <td className="px-4 py-2.5 text-primary">{key.requests_total}</td>
                  <td className="px-4 py-2.5">
                    <KeyACLButton
                      models={models.map((model) => model.id)}
                      selected={key.allowed_models}
                      onSave={(next) => void saveKeyModels(key.id, next)}
                    />
                  </td>
                  <td className="px-4 py-2.5">
                    <button
                      className="danger-button"
                      type="button"
                      onClick={() => {
                        if (window.confirm(`Revoke ${key.label}?`)) {
                          void revokeAPIKey(key.id);
                        }
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
              {keys.length === 0 && (
                <tr>
                  <td className="px-4 py-8 text-center text-muted" colSpan={7}>
                    No active keys.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function ModelACLPicker({
  className = '',
  models,
  selected,
  onChange,
}: {
  className?: string;
  models: string[];
  selected: string[];
  onChange: (next: string[]) => void;
}): JSX.Element {
  const allModels = selected.length === 0;
  const toggle = (model: string) => {
    onChange(selected.includes(model) ? selected.filter((item) => item !== model) : [...selected, model].sort());
  };
  return (
    <div className={className}>
      <div className="mb-2 flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted">
        <ShieldCheck className="h-3.5 w-3.5 text-accent" />
        <span>Allowed Models</span>
        <button className="ml-auto text-[10px] uppercase tracking-wider text-accent" type="button" onClick={() => onChange([])}>
          Allow all
        </button>
      </div>
      <div className="grid gap-1.5 md:grid-cols-2 xl:grid-cols-3">
        {models.map((model) => (
          <label
            key={model}
            className="flex cursor-pointer items-center gap-2 border border-border bg-base px-3 py-2 text-xs text-muted hover:border-accent"
          >
            <input
              type="checkbox"
              checked={allModels || selected.includes(model)}
              onChange={() => toggle(model)}
            />
            <span className={allModels || selected.includes(model) ? 'text-primary' : ''}>{model}</span>
          </label>
        ))}
      </div>
      <div className="mt-2 text-[10px] text-muted">
        {allModels
          ? 'Empty allowlist — key can use every registered model.'
          : `${selected.length} model${selected.length === 1 ? '' : 's'} allowed.`}
      </div>
    </div>
  );
}

function KeyACLButton({
  models,
  selected,
  onSave,
}: {
  models: string[];
  selected: string[];
  onSave: (next: string[]) => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<string[]>(selected ?? []);
  return (
    <div className="min-w-[220px]">
      <button
        className="command-button text-xs"
        type="button"
        onClick={() => {
          setDraft(selected ?? []);
          setOpen((value) => !value);
        }}
      >
        <ShieldCheck className="h-3 w-3" />
        {selected.length === 0 ? 'All models' : `${selected.length} allowed`}
      </button>
      {open && (
        <div className="mt-2 border border-border bg-surface p-3">
          <ModelACLPicker models={models} selected={draft} onChange={setDraft} />
          <div className="mt-3 flex justify-end gap-2">
            <button className="icon-button w-auto px-3 text-xs" type="button" onClick={() => setOpen(false)}>
              Cancel
            </button>
            <button
              className="command-button"
              type="button"
              onClick={() => {
                onSave(draft);
                setOpen(false);
              }}
            >
              Save
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
