import { Check, Copy, KeyRound, Plus, Trash2 } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { useGatewayStore } from '../store/useGatewayStore';

export default function KeyManager(): JSX.Element {
  const [label, setLabel] = useState('');
  const [copied, setCopied] = useState(false);
  const keys = useGatewayStore((state) => state.keys);
  const createdKey = useGatewayStore((state) => state.createdKey);
  const loadKeys = useGatewayStore((state) => state.loadKeys);
  const createAPIKey = useGatewayStore((state) => state.createAPIKey);
  const revokeAPIKey = useGatewayStore((state) => state.revokeAPIKey);

  useEffect(() => {
    void loadKeys();
  }, [loadKeys]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    await createAPIKey(label);
    setLabel('');
    setCopied(false);
  };

  const copyKey = async () => {
    if (!createdKey) {
      return;
    }
    await navigator.clipboard.writeText(createdKey.key);
    setCopied(true);
    toast.success('Key copied');
  };

  return (
    <div className="space-y-5">
      <section className="panel p-5">
        <div className="mb-4 flex items-center gap-3">
          <KeyRound className="h-5 w-5 text-accent" />
          <h2 className="text-sm font-semibold">Create New Key</h2>
        </div>
        <form className="flex flex-col gap-3 sm:flex-row" onSubmit={submit}>
          <input
            className="field min-w-0 flex-1"
            placeholder="Label"
            value={label}
            onChange={(event) => setLabel(event.target.value)}
          />
          <button className="command-button justify-center" type="submit">
            <Plus className="h-4 w-4" />
            Create
          </button>
        </form>
        {createdKey && (
          <div className="mt-4 rounded-panel border border-accent bg-[var(--accent-dim)] p-4">
            <div className="mb-2 text-sm text-primary">New key for {createdKey.label}</div>
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center">
              <code className="min-w-0 flex-1 break-all rounded-panel border border-border bg-base p-3 font-mono text-sm text-accent">
                {createdKey.key}
              </code>
              <button className="command-button justify-center" type="button" onClick={() => void copyKey()}>
                {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
          </div>
        )}
      </section>

      <section className="panel overflow-hidden">
        <div className="border-b border-border p-4">
          <h2 className="text-sm font-semibold">Active API Keys</h2>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] border-collapse text-left text-sm">
            <thead className="border-b border-border text-xs uppercase text-muted">
              <tr>
                <th className="px-4 py-3 font-medium">ID</th>
                <th className="px-4 py-3 font-medium">Label</th>
                <th className="px-4 py-3 font-medium">Created</th>
                <th className="px-4 py-3 font-medium">Last Used</th>
                <th className="px-4 py-3 font-medium">Requests</th>
                <th className="px-4 py-3 font-medium">Action</th>
              </tr>
            </thead>
            <tbody>
              {keys.map((key) => (
                <tr key={key.id} className="border-b border-border transition hover:bg-elevated">
                  <td className="px-4 py-3 font-mono text-muted">{key.id}</td>
                  <td className="px-4 py-3">{key.label}</td>
                  <td className="px-4 py-3 font-mono text-muted">{new Date(key.created_at).toLocaleString()}</td>
                  <td className="px-4 py-3 font-mono text-muted">
                    {key.last_used ? new Date(key.last_used).toLocaleString() : 'Never'}
                  </td>
                  <td className="px-4 py-3 font-mono">{key.requests_total}</td>
                  <td className="px-4 py-3">
                    <button
                      className="danger-button"
                      type="button"
                      onClick={() => {
                        if (window.confirm(`Revoke ${key.label}?`)) {
                          void revokeAPIKey(key.id);
                        }
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
              {keys.length === 0 && (
                <tr>
                  <td className="px-4 py-8 text-center text-muted" colSpan={6}>
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
