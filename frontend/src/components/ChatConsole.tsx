import { Bot, Copy, Eraser, FileText, Loader2, Send, Square, User } from 'lucide-react';
import { FormEvent, KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react';
import toast from 'react-hot-toast';
import { ChatMessage, GatewayAPIError, gatewayErrorMessage, streamChatCompletion } from '../api/client';
import { useGatewayStore } from '../store/useGatewayStore';

interface TranscriptMessage extends ChatMessage {
  id: string;
  routedModel?: string;
  fallback?: boolean;
}

export default function ChatConsole(): JSX.Element {
  const models = useGatewayStore((state) => state.models);
  const templates = useGatewayStore((state) => state.templates);
  const clearToken = useGatewayStore((state) => state.clearToken);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const loadTemplates = useGatewayStore((state) => state.loadTemplates);
  const loadStats = useGatewayStore((state) => state.loadStats);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [messages, setMessages] = useState<TranscriptMessage[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    void loadModels();
    void loadTemplates();
  }, [loadModels, loadTemplates]);

  useEffect(() => {
    if (!selectedModel && models.length > 0) {
      const preferred = models.find((model) => model.id === 'llama3:8b') ?? models[0];
      setSelectedModel(preferred.id);
    }
  }, [models, selectedModel]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, sending]);

  const chatMessages = useMemo<ChatMessage[]>(
    () => messages.map((message) => ({ role: message.role, content: message.content })),
    [messages],
  );

  const submit = async (event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    const content = input.trim();
    if (!content || !selectedModel || sending) return;

    const userMessage: TranscriptMessage = { id: crypto.randomUUID(), role: 'user', content };
    const nextMessages = [...messages, userMessage];
    const template = templates.find((item) => item.id === selectedTemplate);
    const requestMessages: ChatMessage[] = [
      ...(template?.system_prompt ? [{ role: 'system' as const, content: template.system_prompt }] : []),
      ...chatMessages,
      { role: 'user' as const, content },
    ];
    setMessages(nextMessages);
    setInput('');
    setSending(true);
    const assistantID = crypto.randomUUID();
    const controller = new AbortController();
    abortRef.current = controller;

    try {
      setMessages((current) => [...current, { id: assistantID, role: 'assistant', content: '' }]);
      const result = await streamChatCompletion(
        selectedModel,
        requestMessages,
        (token) => {
          setMessages((current) =>
            current.map((message) =>
              message.id === assistantID ? { ...message, content: message.content + token } : message,
            ),
          );
        },
        controller.signal,
      );
      setMessages((current) =>
        current.map((message) =>
          message.id === assistantID
            ? { ...message, content: message.content || result.content, routedModel: result.routedModel, fallback: result.fallback }
            : message,
        ),
      );
      void loadStats();
    } catch (error) {
      setMessages(nextMessages);
      if (error instanceof DOMException && error.name === 'AbortError') {
        toast('Generation stopped');
      } else if (error instanceof GatewayAPIError && error.status === 401) {
        clearToken();
        toast.error('API key rejected. Sign in again.');
      } else {
        toast.error(gatewayErrorMessage(error));
      }
    } finally {
      setSending(false);
      abortRef.current = null;
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      void submit();
    }
  };

  return (
    <div className="grid min-h-[620px] grid-cols-1 gap-4 xl:h-[calc(100vh-112px)] xl:grid-cols-[minmax(0,1fr)_280px]">
      <section className="panel flex min-h-0 flex-col overflow-hidden">
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-widest text-muted">Chat Console</div>
            <div className="truncate text-xs text-accent">{selectedModel || 'No model selected'}</div>
          </div>
          <button className="icon-button" type="button" title="Clear chat" aria-label="Clear chat" onClick={() => setMessages([])}>
            <Eraser className="h-3.5 w-3.5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
          {messages.length === 0 && (
            <div className="flex h-full items-center justify-center text-center">
              <div>
                <div className="text-2xl text-accent">&gt;_</div>
                <div className="mt-2 text-[10px] uppercase tracking-widest text-muted">Ready</div>
              </div>
            </div>
          )}

          {messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))}

          {sending && (
            <div className="flex items-center gap-2 text-xs text-muted">
              <Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />
              <span className="text-[10px] uppercase tracking-wider">Processing</span>
            </div>
          )}
          <div ref={bottomRef} />
        </div>

        <form className="border-t border-border p-3" onSubmit={(event) => void submit(event)}>
          <div className="flex gap-2">
            <textarea
              className="field min-h-[80px] flex-1 resize-none py-2"
              placeholder="Type a message... (Enter to send, Shift+Enter for newline)"
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={onKeyDown}
              disabled={sending}
            />
            <button
              className="command-button h-[80px] w-10 justify-center px-0"
              type={sending ? 'button' : 'submit'}
              aria-label={sending ? 'Stop generation' : 'Send message'}
              title={sending ? 'Stop generation' : 'Send message'}
              disabled={!sending && (!input.trim() || !selectedModel)}
              onClick={() => {
                if (sending) abortRef.current?.abort();
              }}
            >
              {sending ? <Square className="h-3.5 w-3.5" /> : <Send className="h-3.5 w-3.5" />}
            </button>
          </div>
        </form>
      </section>

      <aside className="space-y-3">
        <section className="panel p-3">
          <label className="mb-1.5 block text-[10px] uppercase tracking-wider text-muted" htmlFor="chat-model">
            Model
          </label>
          <select
            id="chat-model"
            className="field w-full"
            value={selectedModel}
            onChange={(event) => setSelectedModel(event.target.value)}
          >
            {models.map((model) => (
              <option key={model.id} value={model.id}>
                {model.id}
              </option>
            ))}
          </select>
        </section>

        <section className="panel p-3">
          <label className="mb-1.5 flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted" htmlFor="chat-template">
            <FileText className="h-3 w-3 text-accent" />
            Template
          </label>
          <select
            id="chat-template"
            className="field w-full"
            value={selectedTemplate}
            onChange={(event) => {
              const template = templates.find((item) => item.id === event.target.value);
              setSelectedTemplate(event.target.value);
              if (template) {
                setInput(template.prompt);
                if (template.model) setSelectedModel(template.model);
              }
            }}
          >
            <option value="">No template</option>
            {templates.map((template) => (
              <option key={template.id} value={template.id}>
                {template.name}
              </option>
            ))}
          </select>
        </section>

        <section className="panel overflow-hidden">
          <div className="border-b border-border px-3 py-2">
            <div className="text-[10px] uppercase tracking-widest text-muted">Models</div>
          </div>
          <div className="divide-y divide-border">
            {models.map((model) => (
              <button
                key={model.id}
                className={`w-full border-l-2 p-3 text-left transition ${
                  model.id === selectedModel
                    ? 'border-accent bg-[var(--accent-dim)]'
                    : 'border-transparent hover:bg-elevated'
                }`}
                type="button"
                onClick={() => setSelectedModel(model.id)}
              >
                <div className="truncate text-xs text-primary">{model.id}</div>
                <div className="mt-0.5 flex items-center justify-between text-[10px] text-muted">
                  <span>{model.vram_gb.toFixed(1)} GB</span>
                  <span className={model.status === 'loaded' ? 'text-success' : 'text-muted'}>{model.status}</span>
                </div>
              </button>
            ))}
          </div>
        </section>
      </aside>
    </div>
  );
}

function MessageBubble({ message }: { message: TranscriptMessage }): JSX.Element {
  const isUser = message.role === 'user';
  const copy = async () => {
    await navigator.clipboard.writeText(message.content);
    toast.success('Copied');
  };

  return (
    <div className={`flex gap-2 ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="mt-1 flex h-7 w-7 shrink-0 items-center justify-center border border-accent bg-[var(--accent-dim)] text-accent">
          <Bot className="h-3.5 w-3.5" />
        </div>
      )}
      <div
        className={`max-w-[80%] border p-3 text-xs leading-6 ${
          isUser ? 'border-accent bg-[var(--accent-dim)]' : 'border-border bg-elevated'
        }`}
      >
        <div className="whitespace-pre-wrap">{message.content}</div>
        <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2 text-[10px] text-muted">
          <span>{isUser ? 'you' : (message.routedModel ?? 'assistant')}</span>
          {message.fallback && <span className="text-warn">fallback</span>}
          <button
            className="ml-auto inline-flex items-center gap-1 text-muted transition hover:text-accent"
            type="button"
            onClick={() => void copy()}
          >
            <Copy className="h-3 w-3" />
            Copy
          </button>
        </div>
      </div>
      {isUser && (
        <div className="mt-1 flex h-7 w-7 shrink-0 items-center justify-center border border-border bg-elevated text-muted">
          <User className="h-3.5 w-3.5" />
        </div>
      )}
    </div>
  );
}
