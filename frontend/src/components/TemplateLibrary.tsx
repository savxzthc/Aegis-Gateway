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
    setForm({ name: template.name, system_prompt: template.system_prompt, prompt: template.prompt, model: template.model });
  };

  return (
    <div className="grid gap-4 xl:grid-cols-[380px_minmax(0,1fr)]">
      <section className="panel">
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <FileText className="h-4 w-4 text-accent" />
          <div className="text-[10px] uppercase tracking-widest text-muted">
            {editing ? 'Edit Template' : 'New Template'}
          </div>
        </div>
        <form className="space-y-3 p-4" onSubmit={submit}>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted">Name</label>
            <input
              className="field w-full"
              placeholder="Template name"
              value={form.name}
              onChange={(event) => setForm({ ...form, name: event.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted">Model</label>
            <select
              className="field w-full"
              value={form.model}
              onChange={(event) => setForm({ ...form, model: event.target.value })}
            >
              <option value="">Any model</option>
              {models.map((model) => (
                <option key={model.id} value={model.id}>
                  {model.id}
                </option>
              ))}
            </select>
          </div>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted">System Prompt</label>
            <textarea
              className="field min-h-[80px] w-full resize-y py-2"
              placeholder="System prompt (optional)"
              value={form.system_prompt}
              onChange={(event) => setForm({ ...form, system_prompt: event.target.value })}
            />
          </div>
          <div className="space-y-1.5">
            <label className="block text-[10px] uppercase tracking-wider text-muted">User Prompt</label>
            <textarea
              className="field min-h-[180px] w-full resize-y py-2"
              placeholder="Reusable user prompt"
              value={form.prompt}
              onChange={(event) => setForm({ ...form, prompt: event.target.value })}
            />
          </div>
          <div className="flex gap-2">
            <button
              className="command-button"
              type="submit"
              disabled={!form.name.trim() || !form.prompt.trim()}
            >
              {editing ? <Save className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
              {editing ? 'Save' : 'Create'}
            </button>
            {editing && (
              <button
                className="icon-button w-auto px-3 text-xs"
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
        <div className="border-b border-border px-4 py-3">
          <div className="text-[10px] uppercase tracking-widest text-muted">Prompt Templates</div>
        </div>
        <div className="divide-y divide-border">
          {templates.map((template) => (
            <article key={template.id} className="p-4 transition hover:bg-elevated">
              <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <div className="text-sm font-bold text-primary">{template.name}</div>
                  <div className="mt-0.5 text-[10px] uppercase tracking-wider text-muted">
                    {template.model || 'any model'}
                  </div>
                  {template.system_prompt && (
                    <p className="mt-2 line-clamp-2 text-xs text-muted">{template.system_prompt}</p>
                  )}
                  <p className="mt-1 line-clamp-3 whitespace-pre-wrap text-xs text-primary">{template.prompt}</p>
                </div>
                <div className="flex shrink-0 gap-2">
                  <button className="icon-button" type="button" title="Copy prompt" onClick={() => void copyTemplate(template)}>
                    <Copy className="h-3.5 w-3.5" />
                  </button>
                  <button className="command-button" type="button" onClick={() => edit(template)}>
                    Edit
                  </button>
                  <button
                    className="danger-button"
                    type="button"
                    onClick={() => {
                      if (window.confirm(`Delete ${template.name}?`)) {
                        void deletePromptTemplate(template.id);
                      }
                    }}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Delete
                  </button>
                </div>
              </div>
            </article>
          ))}
          {templates.length === 0 && (
            <div className="p-8 text-center text-xs text-muted">No prompt templates yet.</div>
          )}
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
