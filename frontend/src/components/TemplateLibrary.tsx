import { Copy, FileText, Plus, Save, Trash2 } from 'lucide-react';
import { FormEvent, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { PromptTemplate, TemplatePayload } from '../api/client';
import { useGatewayStore } from '../store/useGatewayStore';

const emptyPayload: TemplatePayload = { name: '', system_prompt: '', prompt: '', model: '' };

export default function TemplateLibrary(): JSX.Element {
  const templates = useGatewayStore((state) => state.templates);
  const models = useGatewayStore((state) => state.models);
  const loadTemplates = useGatewayStore((state) => state.loadTemplates);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const createPromptTemplate = useGatewayStore((state) => state.createPromptTemplate);
  const updatePromptTemplate = useGatewayStore((state) => state.updatePromptTemplate);
  const deletePromptTemplate = useGatewayStore((state) => state.deletePromptTemplate);
  const [editing, setEditing] = useState<PromptTemplate | null>(null);
  const [form, setForm] = useState<TemplatePayload>(emptyPayload);

  useEffect(() => {
    void loadTemplates();
    void loadModels();
  }, [loadTemplates, loadModels]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (editing) {
      await updatePromptTemplate(editing.id, form);
      toast.success('Template updated');
    } else {
      await createPromptTemplate(form);
      toast.success('Template created');
    }
    setEditing(null);
    setForm(emptyPayload);
  };

  const edit = (template: PromptTemplate) => {
    setEditing(template);
    setForm({
      name: template.name,
      system_prompt: template.system_prompt,
      prompt: template.prompt,
      model: template.model,
    });
  };

  return (
    <div className="grid gap-5 xl:grid-cols-[420px_minmax(0,1fr)]">
      <section className="panel p-5">
        <div className="mb-4 flex items-center gap-3">
          <FileText className="h-5 w-5 text-accent" />
          <h2 className="text-sm font-semibold">{editing ? 'Edit Template' : 'Create Template'}</h2>
        </div>
        <form className="space-y-3" onSubmit={submit}>
          <input className="field w-full" placeholder="Name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
          <select className="field w-full font-mono" value={form.model} onChange={(event) => setForm({ ...form, model: event.target.value })}>
            <option value="">No model preference</option>
            {models.map((model) => (
              <option key={model.id} value={model.id}>
                {model.id}
              </option>
            ))}
          </select>
          <textarea
            className="field min-h-[96px] w-full resize-y"
            placeholder="System prompt"
            value={form.system_prompt}
            onChange={(event) => setForm({ ...form, system_prompt: event.target.value })}
          />
          <textarea
            className="field min-h-[220px] w-full resize-y"
            placeholder="Reusable user prompt"
            value={form.prompt}
            onChange={(event) => setForm({ ...form, prompt: event.target.value })}
          />
          <div className="flex gap-2">
            <button className="command-button justify-center" type="submit" disabled={!form.name.trim() || !form.prompt.trim()}>
              {editing ? <Save className="h-4 w-4" /> : <Plus className="h-4 w-4" />}
              {editing ? 'Save' : 'Create'}
            </button>
            {editing && (
              <button
                className="icon-button px-3"
                type="button"
                onClick={() => {
                  setEditing(null);
                  setForm(emptyPayload);
                }}
              >
                Cancel
              </button>
            )}
          </div>
        </form>
      </section>

      <section className="panel overflow-hidden">
        <div className="border-b border-border p-4">
          <h2 className="text-sm font-semibold">Prompt Templates</h2>
        </div>
        <div className="divide-y divide-border">
          {templates.map((template) => (
            <article key={template.id} className="p-4 transition hover:bg-elevated">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <div className="font-semibold">{template.name}</div>
                  <div className="mt-1 font-mono text-xs text-muted">{template.model || 'any model'}</div>
                  {template.system_prompt && <p className="mt-3 line-clamp-2 text-sm text-muted">{template.system_prompt}</p>}
                  <p className="mt-2 line-clamp-3 whitespace-pre-wrap text-sm">{template.prompt}</p>
                </div>
                <div className="flex shrink-0 gap-2">
                  <button className="icon-button" type="button" title="Copy prompt" onClick={() => void copyTemplate(template)}>
                    <Copy className="h-4 w-4" />
                  </button>
                  <button className="command-button h-9 px-3 text-xs" type="button" onClick={() => edit(template)}>
                    Edit
                  </button>
                  <button
                    className="danger-button h-9 px-3 text-xs"
                    type="button"
                    onClick={() => {
                      if (window.confirm(`Delete ${template.name}?`)) {
                        void deletePromptTemplate(template.id);
                      }
                    }}
                  >
                    <Trash2 className="h-4 w-4" />
                    Delete
                  </button>
                </div>
              </div>
            </article>
          ))}
          {templates.length === 0 && <div className="p-8 text-center text-muted">No prompt templates yet.</div>}
        </div>
      </section>
    </div>
  );
}

async function copyTemplate(template: PromptTemplate): Promise<void> {
  const value = [template.system_prompt, template.prompt].filter(Boolean).join('\n\n');
  await navigator.clipboard.writeText(value);
  toast.success('Template copied');
}
