import { Bot, Copy, Eraser, Loader2, Send, User } from 'lucide-react';
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
  const clearToken = useGatewayStore((state) => state.clearToken);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const loadStats = useGatewayStore((state) => state.loadStats);
  const [selectedModel, setSelectedModel] = useState('');
  const [messages, setMessages] = useState<TranscriptMessage[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    void loadModels();
  }, [loadModels]);

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
    if (!content || !selectedModel || sending) {
      return;
    }

    const userMessage: TranscriptMessage = {
      id: crypto.randomUUID(),
      role: 'user',
      content,
    };
    const nextMessages = [...messages, userMessage];
    setMessages(nextMessages);
    setInput('');
    setSending(true);
    const assistantID = crypto.randomUUID();
    const controller = new AbortController();
    abortRef.current = controller;

    try {
      setMessages((current) => [
        ...current,
        {
          id: assistantID,
          role: 'assistant',
          content: '',
        },
      ]);
      const result = await streamChatCompletion(
        selectedModel,
        [...chatMessages, { role: 'user', content }],
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
            ? {
                ...message,
                content: message.content || result.content,
                routedModel: result.routedModel,
                fallback: result.fallback,
              }
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
    <div className="grid min-h-[620px] grid-cols-1 gap-5 xl:h-[calc(100vh-128px)] xl:grid-cols-[minmax(0,1fr)_320px]">
      <section className="panel flex min-h-0 flex-col overflow-hidden">
        <div className="flex items-center justify-between gap-3 border-b border-border p-4">
          <div className="min-w-0">
            <h2 className="text-sm font-semibold">Chat</h2>
            <div className="mt-1 truncate font-mono text-xs text-muted">{selectedModel || 'No model selected'}</div>
          </div>
          <button className="icon-button" type="button" title="Clear chat" aria-label="Clear chat" onClick={() => setMessages([])}>
            <Eraser className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
          {messages.length === 0 && (
            <div className="flex h-full items-center justify-center text-center">
              <div>
                <Bot className="mx-auto mb-3 h-8 w-8 text-accent" />
                <div className="font-mono text-sm text-muted">Ready</div>
              </div>
            </div>
          )}

          {messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))}

          {sending && (
            <div className="flex items-center gap-2 font-mono text-sm text-muted">
              <Loader2 className="h-4 w-4 animate-spin text-accent" />
              Running local model
            </div>
          )}
          <div ref={bottomRef} />
        </div>

        <form className="border-t border-border p-4" onSubmit={(event) => void submit(event)}>
          <div className="flex gap-3">
            <textarea
              className="field min-h-[88px] flex-1 resize-none py-3"
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={onKeyDown}
              disabled={sending}
            />
            <button
              className="command-button h-[88px] w-12 justify-center px-0"
              type={sending ? 'button' : 'submit'}
              aria-label={sending ? 'Stop generation' : 'Send message'}
              title={sending ? 'Stop generation' : 'Send message'}
              disabled={!sending && (!input.trim() || !selectedModel)}
              onClick={() => {
                if (sending) {
                  abortRef.current?.abort();
                }
              }}
            >
              {sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
            </button>
          </div>
        </form>
      </section>

      <aside className="space-y-5">
        <section className="panel p-4">
          <label className="mb-2 block text-sm text-muted" htmlFor="chat-model">
            Model
          </label>
          <select
            id="chat-model"
            className="field w-full font-mono"
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

        <section className="panel p-4">
          <h2 className="mb-3 text-sm font-semibold">Models</h2>
          <div className="space-y-2">
            {models.map((model) => (
              <button
                key={model.id}
                className={`w-full rounded-panel border p-3 text-left transition ${
                  model.id === selectedModel
                    ? 'border-accent bg-[var(--accent-dim)]'
                    : 'border-border bg-base hover:bg-elevated'
                }`}
                type="button"
                onClick={() => setSelectedModel(model.id)}
              >
                <div className="truncate font-mono text-sm text-primary">{model.id}</div>
                <div className="mt-1 flex items-center justify-between gap-2 font-mono text-xs text-muted">
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
    <div className={`flex gap-3 ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && (
        <div className="mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-panel border border-accent bg-[var(--accent-dim)] text-accent">
          <Bot className="h-4 w-4" />
        </div>
      )}
      <div className={`max-w-[78%] rounded-panel border p-4 ${isUser ? 'border-accent bg-[var(--accent-dim)]' : 'border-border bg-elevated'}`}>
        <div className="whitespace-pre-wrap text-sm leading-6">{message.content}</div>
        <div className="mt-3 flex flex-wrap items-center gap-2 border-t border-border pt-3 font-mono text-xs text-muted">
          <span>{isUser ? 'you' : message.routedModel ?? 'assistant'}</span>
          {message.fallback && <span className="text-warn">fallback</span>}
          <button className="ml-auto inline-flex items-center gap-1 text-muted transition hover:text-accent" type="button" onClick={() => void copy()}>
            <Copy className="h-3.5 w-3.5" />
            Copy
          </button>
        </div>
      </div>
      {isUser && (
        <div className="mt-1 flex h-8 w-8 shrink-0 items-center justify-center rounded-panel border border-border bg-elevated text-primary">
          <User className="h-4 w-4" />
        </div>
      )}
    </div>
  );
}
