import { GitCompareArrows, Loader2 } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import toast from 'react-hot-toast';
import { gatewayErrorMessage } from '../api/client';
import { useGatewayStore } from '../store/useGatewayStore';

export default function Compare(): JSX.Element {
  const models = useGatewayStore((state) => state.models);
  const active = useGatewayStore((state) => state.activeComparison);
  const history = useGatewayStore((state) => state.comparisons);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const loadComparisons = useGatewayStore((state) => state.loadComparisons);
  const startComparison = useGatewayStore((state) => state.startComparison);
  const voteComparison = useGatewayStore((state) => state.voteComparison);
  const [prompt, setPrompt] = useState('');
  const [modelA, setModelA] = useState('');
  const [modelB, setModelB] = useState('');
  const [blind, setBlind] = useState(true);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    void Promise.all([loadModels(), loadComparisons()]);
  }, [loadModels, loadComparisons]);

  useEffect(() => {
    if (models.length > 0 && !modelA) setModelA(models[0].id);
    if (models.length > 1 && !modelB) setModelB(models[1].id);
  }, [models, modelA, modelB]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!prompt.trim() || !modelA || !modelB || modelA === modelB) return;
    setLoading(true);
    try {
      await startComparison(prompt.trim(), modelA, modelB, blind);
    } catch (error) {
      toast.error(gatewayErrorMessage(error));
    } finally {
      setLoading(false);
    }
  };

  const vote = async (winner: 'a' | 'b' | 'tie') => {
    if (!active) return;
    try {
      await voteComparison(active.id, winner);
    } catch (error) {
      toast.error(gatewayErrorMessage(error));
    }
  };

  return (
    <div className="space-y-4">
      <form className="panel p-4" onSubmit={submit}>
        <div className="mb-3 flex items-center gap-2 text-[10px] uppercase tracking-widest text-muted">
          <GitCompareArrows className="h-3.5 w-3.5 text-accent" />Model comparison
        </div>
        <textarea className="field min-h-28 w-full resize-y py-2" value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="Enter one prompt for both models..." />
        <div className="mt-3 grid gap-3 md:grid-cols-2">
          <select className="field" value={modelA} onChange={(event) => setModelA(event.target.value)}>
            {models.map((model) => <option key={model.id} value={model.id}>{model.id}</option>)}
          </select>
          <select className="field" value={modelB} onChange={(event) => setModelB(event.target.value)}>
            {models.map((model) => <option key={model.id} value={model.id}>{model.id}</option>)}
          </select>
        </div>
        <div className="mt-3 flex items-center justify-between">
          <label className="flex items-center gap-2 text-xs text-muted"><input type="checkbox" checked={blind} onChange={(event) => setBlind(event.target.checked)} />Blind test</label>
          <button className="command-button" disabled={loading || modelA === modelB || !prompt.trim()} type="submit">
            {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <GitCompareArrows className="h-3.5 w-3.5" />}
            Compare
          </button>
        </div>
      </form>

      {active && (
        <section className="space-y-3">
          <div className="grid gap-3 lg:grid-cols-2">
            <ResponsePane label={active.model_a} content={active.response_a} />
            <ResponsePane label={active.model_b} content={active.response_b} />
          </div>
          {!active.winner && (
            <div className="flex justify-center gap-2">
              <button className="command-button" type="button" onClick={() => void vote('a')}>Left won</button>
              <button className="command-button" type="button" onClick={() => void vote('tie')}>Tie</button>
              <button className="command-button" type="button" onClick={() => void vote('b')}>Right won</button>
            </div>
          )}
        </section>
      )}

      <section className="panel">
        <div className="border-b border-border px-4 py-3 text-[10px] uppercase tracking-widest text-muted">Recent comparisons</div>
        <div className="divide-y divide-border">
          {history.map((item) => (
            <div key={item.id} className="flex items-center gap-3 px-4 py-3 text-xs">
              <span className="min-w-0 flex-1 truncate">{item.prompt}</span>
              <span className="text-muted">{item.model_a} vs {item.model_b}</span>
              <span className="pill border-border text-muted">{item.winner ?? 'unvoted'}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function ResponsePane({ label, content }: { label: string; content: string }): JSX.Element {
  return (
    <article className="panel min-h-64 p-4">
      <div className="mb-3 border-b border-border pb-2 text-xs font-bold text-accent">{label}</div>
      <ReactMarkdown>{content}</ReactMarkdown>
    </article>
  );
}
