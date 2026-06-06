import {
  Bot, Copy, Download, Eraser, FileText, Loader2, Paperclip, Search, Send, Square, Trash2, User,
} from 'lucide-react';
import { FormEvent, KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import toast from 'react-hot-toast';
import {
  ChatContent, ChatMessage, GatewayAPIError, contentText, exportConversation, gatewayErrorMessage,
  streamChatCompletion, uploadFile,
} from '../api/client';
import { useGatewayStore } from '../store/useGatewayStore';

const requestWindow = 40;

interface TranscriptMessage extends ChatMessage {
  id: string;
  routedModel?: string;
  fallback?: boolean;
  tokensPerSecond?: number;
  searchUsed?: boolean;
  startedAt?: number;
}

export default function ChatConsole(): JSX.Element {
  const models = useGatewayStore((state) => state.models);
  const templates = useGatewayStore((state) => state.templates);
  const config = useGatewayStore((state) => state.config);
  const conversations = useGatewayStore((state) => state.conversations);
  const activeConversationId = useGatewayStore((state) => state.activeConversationId);
  const clearToken = useGatewayStore((state) => state.clearToken);
  const loadModels = useGatewayStore((state) => state.loadModels);
  const loadTemplates = useGatewayStore((state) => state.loadTemplates);
  const loadStats = useGatewayStore((state) => state.loadStats);
  const loadConversations = useGatewayStore((state) => state.loadConversations);
  const loadConversation = useGatewayStore((state) => state.loadConversation);
  const deleteConversation = useGatewayStore((state) => state.deleteConversation);
  const patchConversation = useGatewayStore((state) => state.patchConversation);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [messages, setMessages] = useState<TranscriptMessage[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [searchEnabled, setSearchEnabled] = useState(false);
  const [upload, setUpload] = useState<{ url: string; preview: string } | null>(null);
  const [uploading, setUploading] = useState(false);
  const [titleDraft, setTitleDraft] = useState('');
  const [clock, setClock] = useState(Date.now());
  const bottomRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const fileRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    void Promise.all([loadModels(), loadTemplates(), loadConversations()]);
  }, [loadModels, loadTemplates, loadConversations]);

  useEffect(() => () => abortRef.current?.abort(), []);

  useEffect(() => {
    if (!sending) return;
    const id = window.setInterval(() => setClock(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [sending]);

  useEffect(() => {
    if (!selectedModel && models.length > 0) {
      setSelectedModel((models.find((model) => model.id === 'llama3:8b') ?? models[0]).id);
    }
  }, [models, selectedModel]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages, sending]);

  const activeConversation = conversations.find((item) => item.id === activeConversationId);
  useEffect(() => setTitleDraft(activeConversation?.title ?? ''), [activeConversation?.title]);

  const chatMessages = useMemo<ChatMessage[]>(
    () => messages.map((message) => ({ role: message.role, content: message.content })),
    [messages],
  );
  const windowedMessages = chatMessages.slice(-requestWindow);
  const truncatedCount = Math.max(0, chatMessages.length - windowedMessages.length);

  const openConversation = async (id: string) => {
    const item = await loadConversation(id);
    setSelectedModel(item.model || selectedModel);
    setMessages(item.messages.map((message) => ({
      id: message.id,
      role: message.role,
      content: message.content,
      routedModel: message.model_used,
      tokensPerSecond: message.tokens_per_second,
    })));
  };

  const newChat = () => {
    setMessages([]);
    setUpload(null);
    useGatewayStore.setState({ activeConversationId: '', activeConversation: null });
  };

  const submit = async (event?: FormEvent<HTMLFormElement>) => {
    event?.preventDefault();
    const text = input.trim();
    if ((!text && !upload) || !selectedModel || sending) return;

    const content: ChatContent = upload
      ? [
          { type: 'image_url', image_url: { url: upload.url } },
          ...(text ? [{ type: 'text' as const, text }] : []),
        ]
      : text;
    const userMessage: TranscriptMessage = { id: crypto.randomUUID(), role: 'user', content };
    const previousMessages = messages;
    const nextMessages = [...messages, userMessage];
    const template = templates.find((item) => item.id === selectedTemplate);
    const requestMessages: ChatMessage[] = [
      ...(template?.system_prompt ? [{ role: 'system' as const, content: template.system_prompt }] : []),
      ...windowedMessages,
      { role: 'user' as const, content },
    ];
    setMessages(nextMessages);
    setInput('');
    setUpload(null);
    setSending(true);
    const assistantID = crypto.randomUUID();
    const controller = new AbortController();
    abortRef.current = controller;

    try {
      setMessages((current) => [...current, { id: assistantID, role: 'assistant', content: '', startedAt: Date.now() }]);
      const result = await streamChatCompletion(
        selectedModel,
        requestMessages,
        (token) => setMessages((current) => current.map((message) =>
          message.id === assistantID ? { ...message, content: `${contentText(message.content)}${token}` } : message,
        )),
        controller.signal,
        { conversationId: activeConversationId || undefined, persist: !activeConversationId, search: searchEnabled },
      );
      setMessages((current) => current.map((message) => message.id === assistantID ? {
        ...message,
        content: contentText(message.content) || result.content,
        routedModel: result.routedModel,
        fallback: result.fallback,
        tokensPerSecond: result.tokensPerSecond,
        searchUsed: result.searchUsed,
      } : message));
      await loadConversations();
      if (result.conversationId) {
        await openConversation(result.conversationId);
      }
      void loadStats();
    } catch (error) {
      setMessages(previousMessages);
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

  const chooseFile = async (file?: File) => {
    if (!file) return;
    setUploading(true);
    try {
      const result = await uploadFile(file);
      setUpload({ url: result.url, preview: URL.createObjectURL(file) });
    } catch (error) {
      toast.error(gatewayErrorMessage(error));
    } finally {
      setUploading(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      void submit();
    }
  };

  return (
    <div className="grid min-h-[620px] grid-cols-1 gap-4 xl:h-[calc(100vh-112px)] xl:grid-cols-[240px_minmax(0,1fr)_260px]">
      <aside className="panel min-h-0 overflow-hidden">
        <div className="flex items-center justify-between border-b border-border px-3 py-2.5">
          <span className="text-[10px] uppercase tracking-widest text-muted">Conversations</span>
          <button className="command-button" type="button" onClick={newChat}>New</button>
        </div>
        <div className="max-h-[300px] overflow-y-auto xl:max-h-full">
          {conversations.map((conversation) => (
            <div key={conversation.id} className={`border-b border-border p-2 ${conversation.id === activeConversationId ? 'bg-[var(--accent-dim)]' : ''}`}>
              <button className="w-full text-left" type="button" onClick={() => void openConversation(conversation.id)}>
                <div className="truncate text-xs text-primary">{conversation.title}</div>
                <div className="mt-1 flex justify-between text-[9px] text-muted">
                  <span className="truncate">{conversation.model || 'auto'}</span>
                  <span>{relativeDate(conversation.updated_at)}</span>
                </div>
              </button>
              <button className="mt-1 text-[9px] text-danger" type="button" onClick={() => void deleteConversation(conversation.id)}>
                delete
              </button>
            </div>
          ))}
        </div>
      </aside>

      <section className="panel flex min-h-0 flex-col overflow-hidden">
        <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          <div className="min-w-0 flex-1">
            {activeConversationId ? (
              <input
                className="field h-7 w-full"
                value={titleDraft}
                onChange={(event) => setTitleDraft(event.target.value)}
                onBlur={() => {
                  if (titleDraft.trim() && titleDraft !== activeConversation?.title) {
                    void patchConversation(activeConversationId, { title: titleDraft.trim() });
                  }
                }}
              />
            ) : (
              <div className="truncate text-xs text-accent">New conversation</div>
            )}
            <div className="mt-1 text-[9px] text-muted">{selectedModel || 'No model selected'}</div>
          </div>
          {activeConversationId && (
            <button className="icon-button" type="button" title="Export markdown" onClick={() => void exportConversation(activeConversationId, 'markdown')}>
              <Download className="h-3.5 w-3.5" />
            </button>
          )}
          <button className="icon-button" type="button" title="Clear chat" onClick={newChat}><Eraser className="h-3.5 w-3.5" /></button>
        </div>

        {truncatedCount > 0 && (
          <div className="border-b border-warn px-4 py-1.5 text-[10px] text-warn">
            Sending the most recent {requestWindow} messages; {truncatedCount} older messages remain visible only.
          </div>
        )}

        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
          {messages.length === 0 && <div className="flex h-full items-center justify-center text-[10px] uppercase tracking-widest text-muted">Ready</div>}
          {messages.map((message) => <MessageBubble key={message.id} message={message} now={clock} />)}
          {sending && <div className="flex items-center gap-2 text-xs text-muted"><Loader2 className="h-3.5 w-3.5 animate-spin text-accent" />Processing</div>}
          <div ref={bottomRef} />
        </div>

        <form className="border-t border-border p-3" onSubmit={(event) => void submit(event)}>
          {upload && (
            <div className="mb-2 flex items-center gap-2 border border-border bg-elevated p-2">
              <img className="h-16 w-16 object-cover" src={upload.preview} alt="Upload preview" />
              <span className="text-[10px] text-muted">Image attached</span>
              <button className="ml-auto text-danger" type="button" onClick={() => setUpload(null)}><Trash2 className="h-3.5 w-3.5" /></button>
            </div>
          )}
          <div className="mb-2 flex gap-2">
            <input ref={fileRef} className="hidden" type="file" accept="image/jpeg,image/png,image/gif,image/webp" onChange={(event) => void chooseFile(event.target.files?.[0])} />
            <button className="icon-button" type="button" disabled={uploading} title="Attach image" onClick={() => fileRef.current?.click()}>
              {uploading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Paperclip className="h-3.5 w-3.5" />}
            </button>
            {config?.config.search.enabled && (
              <button className={`icon-button ${searchEnabled ? 'border-accent text-accent' : ''}`} type="button" title="Web search" onClick={() => setSearchEnabled((value) => !value)}>
                <Search className="h-3.5 w-3.5" />
              </button>
            )}
            {searchEnabled && <span className="pill border-accent text-accent">web grounded</span>}
          </div>
          <div className="flex gap-2">
            <textarea className="field min-h-[80px] flex-1 resize-none py-2" placeholder="Type a message..." value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={onKeyDown} disabled={sending} />
            <button
              className="command-button h-[80px] w-10 justify-center px-0"
              type={sending ? 'button' : 'submit'}
              disabled={!sending && (!input.trim() && !upload)}
              onClick={() => sending && abortRef.current?.abort()}
            >
              {sending ? <Square className="h-3.5 w-3.5" /> : <Send className="h-3.5 w-3.5" />}
            </button>
          </div>
        </form>
      </section>

      <aside className="space-y-3">
        <section className="panel p-3">
          <label className="mb-1.5 block text-[10px] uppercase tracking-wider text-muted" htmlFor="chat-model">Model</label>
          <select id="chat-model" className="field w-full" value={selectedModel} onChange={(event) => setSelectedModel(event.target.value)}>
            {models.map((model) => <option key={model.id} value={model.id}>{model.id}</option>)}
          </select>
        </section>
        <section className="panel p-3">
          <label className="mb-1.5 flex items-center gap-2 text-[10px] uppercase tracking-wider text-muted" htmlFor="chat-template"><FileText className="h-3 w-3 text-accent" />Template</label>
          <select id="chat-template" className="field w-full" value={selectedTemplate} onChange={(event) => {
            const template = templates.find((item) => item.id === event.target.value);
            setSelectedTemplate(event.target.value);
            if (template) {
              setInput(template.prompt);
              if (template.model) setSelectedModel(template.model);
            }
          }}>
            <option value="">No template</option>
            {templates.map((template) => <option key={template.id} value={template.id}>{template.name}</option>)}
          </select>
        </section>
      </aside>
    </div>
  );
}

function MessageBubble({ message, now }: { message: TranscriptMessage; now: number }): JSX.Element {
  const isUser = message.role === 'user';
  const text = contentText(message.content);
  const image = typeof message.content === 'string' ? '' : message.content.find((part) => part.type === 'image_url')?.image_url.url ?? '';
  const liveTPS = !message.tokensPerSecond && message.startedAt && text
    ? Math.max(0, Math.round((text.length / 4) / Math.max(1, (now - message.startedAt) / 1000) * 10) / 10)
    : 0;
  return (
    <div className={`flex gap-2 ${isUser ? 'justify-end' : 'justify-start'}`}>
      {!isUser && <div className="mt-1 flex h-7 w-7 shrink-0 items-center justify-center border border-accent bg-[var(--accent-dim)] text-accent"><Bot className="h-3.5 w-3.5" /></div>}
      <div className={`max-w-[85%] border p-3 text-xs leading-6 ${isUser ? 'border-accent bg-[var(--accent-dim)]' : 'border-border bg-elevated'}`}>
        {image && <img className="mb-2 max-h-72 max-w-full object-contain" src={image} alt="Attached" />}
        {isUser ? <div className="whitespace-pre-wrap">{text}</div> : <div className="prose-aegis"><ReactMarkdown>{text}</ReactMarkdown></div>}
        <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2 text-[10px] text-muted">
          <span>{isUser ? 'you' : (message.routedModel ?? 'assistant')}</span>
          {message.fallback && <span className="text-warn">fallback</span>}
          {message.searchUsed && <span className="text-accent">web-grounded</span>}
          {(message.tokensPerSecond || liveTPS) > 0 && <span>{(message.tokensPerSecond || liveTPS).toFixed(1)} tok/s</span>}
          <button className="ml-auto inline-flex items-center gap-1 hover:text-accent" type="button" onClick={() => void navigator.clipboard.writeText(text).then(() => toast.success('Copied'))}>
            <Copy className="h-3 w-3" />Copy
          </button>
        </div>
      </div>
      {isUser && <div className="mt-1 flex h-7 w-7 shrink-0 items-center justify-center border border-border bg-elevated text-muted"><User className="h-3.5 w-3.5" /></div>}
    </div>
  );
}

function relativeDate(value: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
  if (seconds < 60) return 'now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`;
  return `${Math.floor(seconds / 86400)}d`;
}
