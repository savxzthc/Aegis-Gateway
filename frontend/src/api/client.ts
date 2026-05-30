import axios, { AxiosError } from 'axios';

export interface HardwareInfo {
  gpu_name: string;
  vram_total_gb: number;
  vram_free_gb: number;
  vram_used_gb: number;
  temperature_c?: number;
  detected: boolean;
}

export interface ModelInfo {
  id: string;
  object: string;
  created: number;
  owned_by: string;
  vram_gb: number;
  backend: 'ollama' | 'llamacpp';
  description: string;
  status: 'unloaded' | 'loading' | 'loaded' | 'idle';
}

export interface ModelListResponse {
  object: 'list';
  data: ModelInfo[];
}

export interface CatalogModel {
  id: string;
  display_name: string;
  backend: 'ollama' | 'llamacpp';
  vram_gb: number;
  size_gb: number;
  description: string;
  use_case: string;
  category: CatalogCategory;
  library_url: string;
  installed: boolean;
  registered: boolean;
  status: 'available' | 'downloading' | 'installed' | 'failed';
  progress_pct: number;
  message: string;
}

export type CatalogCategory = 'standard' | 'abliterated';

export interface ModelCatalogResponse {
  online: boolean;
  data: CatalogModel[];
  total: number;
  limit: number;
  offset: number;
  category: CatalogCategory;
}

export interface PullJob {
  model: string;
  status: 'downloading' | 'installed' | 'failed';
  progress_pct: number;
  message: string;
  started_at: string;
  completed_at?: string;
  error?: string;
}

export interface TopModel {
  model: string;
  count: number;
}

export interface HourlyRequest {
  hour: string;
  count: number;
}

export interface StatsResponse {
  requests_today: number;
  requests_yesterday: number;
  requests_total: number;
  avg_latency_ms: number;
  top_models: TopModel[];
  fallback_rate_pct: number;
  requests_per_hour: HourlyRequest[];
  active_model: string;
}

export interface RequestLog {
  id: number;
  timestamp: string;
  key_id: string;
  model_requested: string;
  model_used: string;
  fallback_triggered: boolean;
  backend_type: string;
  latency_ms: number;
  estimated_prompt_tokens: number;
  estimated_completion_tokens: number;
  status_code: number;
}

export interface LogsResponse {
  data: RequestLog[];
  limit: number;
  offset: number;
  total: number;
}

export interface APIKey {
  id: string;
  label: string;
  created_at: string;
  last_used: string | null;
  requests_total: number;
  allowed_models: string[];
}

export interface KeysResponse {
  data: APIKey[];
}

export interface CreateKeyResponse {
  id: string;
  label: string;
  created_at: string;
  key: string;
}

export interface PromptTemplate {
  id: string;
  name: string;
  system_prompt: string;
  prompt: string;
  model: string;
  created_at: string;
  updated_at: string;
}

export interface TemplatesResponse {
  data: PromptTemplate[];
}

export interface TemplatePayload {
  name: string;
  system_prompt: string;
  prompt: string;
  model: string;
}

export interface ModelRegistryEntry {
  vram_gb: number;
  backend: 'ollama' | 'llamacpp';
  description: string;
}

export interface GatewayConfig {
  server: {
    host: string;
    port: number;
    idle_timeout_minutes: number;
    request_timeout_seconds: number;
  };
  security: {
    rate_limit_rpm: number;
    auto_generate_key: boolean;
  };
  backend: {
    default_type: 'ollama' | 'llamacpp';
    ollama_base_url: string;
    llamacpp_base_url: string;
  };
  models: {
    registry: Record<string, ModelRegistryEntry>;
  };
}

export interface RuntimeInfo {
  go_version: string;
  uptime_sec: number;
  build_time: string;
  app_version: string;
}

export interface ConfigResponse {
  config: GatewayConfig;
  runtime: RuntimeInfo;
  restart_required: boolean;
}

export interface ConfigPatch {
  port?: number;
  idle_timeout_minutes?: number;
  rate_limit_rpm?: number;
  ollama_base_url?: string;
}

export type ChatRole = 'system' | 'user' | 'assistant' | 'tool';

export interface ChatMessage {
  role: ChatRole;
  content: string;
}

export interface ChatCompletionRequest {
  model: string;
  messages: ChatMessage[];
  stream: false;
}

export interface ChatCompletionResponse {
  id: string;
  object: 'chat.completion';
  created: number;
  model: string;
  choices: Array<{
    index: number;
    message: ChatMessage;
    finish_reason: string;
  }>;
  usage: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
}

export interface ChatResult {
  content: string;
  model: string;
  routedModel: string;
  fallback: boolean;
  usage: ChatCompletionResponse['usage'];
}

export interface ChatCompletionChunk {
  id: string;
  object: 'chat.completion.chunk';
  created: number;
  model: string;
  usage?: ChatCompletionResponse['usage'];
  choices: Array<{
    index: number;
    delta: Partial<ChatMessage>;
    finish_reason?: string;
  }>;
}

export interface APIErrorResponse {
  error: string;
  code: string;
}

export class GatewayAPIError extends Error {
  status: number;
  code: string;

  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = 'GatewayAPIError';
    this.status = status;
    this.code = code;
  }
}

const client = axios.create({
  baseURL: '/v1',
  timeout: 0,
});

let currentAuthToken = '';

client.interceptors.response.use(
  (response) => response,
  (error: AxiosError<APIErrorResponse>) => {
    const message = error.response?.data?.error ?? error.message;
    const status = error.response?.status ?? 0;
    const code = error.response?.data?.code ?? (error.response ? 'REQUEST_FAILED' : 'NETWORK_UNAVAILABLE');
    return Promise.reject(new GatewayAPIError(message, status, code));
  },
);

export function gatewayErrorMessage(error: unknown): string {
  if (error instanceof GatewayAPIError) {
    switch (error.code) {
      case 'NETWORK_UNAVAILABLE':
        return 'Aegis is unreachable. Start the gateway and open the dashboard URL printed in the terminal.';
      case 'MODEL_LOAD_FAILED':
        return 'Aegis could not load this model. Make sure Ollama is running and the model is installed locally.';
      case 'BACKEND_UNAVAILABLE':
        return 'The configured model backend is unavailable. Check Settings and restart Aegis after changing backend URLs.';
      case 'BACKEND_ERROR':
      case 'BACKEND_STREAM_ERROR':
        return 'The local model runner did not complete the request. Make sure Ollama is running and the selected model is installed.';
      case 'MODEL_NOT_REGISTERED':
        return 'That model is not registered in config.toml. Add it to the model registry, then restart Aegis.';
      case 'MODEL_NOT_ALLOWED':
        return 'This API key is not allowed to use that model. Update the key allowlist in Keys.';
      case 'MODEL_NOT_IN_CATALOG':
        return 'That model is not available in the Aegis download catalog.';
      case 'MODEL_LIBRARY_OFFLINE':
        return 'The Ollama model library is not reachable. Check your internet connection and try again.';
      case 'MODEL_PULL_START_FAILED':
        return 'Aegis could not start the model download. Make sure Ollama is installed, running, and available on PATH.';
      case 'MODEL_REGISTER_FAILED':
        return 'Aegis could not register the model in config.toml before downloading.';
      case 'NO_MODEL_FITS':
        return 'No registered model fits the currently available VRAM. Close GPU-heavy apps or add a smaller fallback model.';
      case 'HARDWARE_QUERY_FAILED':
        return 'Aegis could not read hardware status. Chat can still work, but VRAM routing may fall back conservatively.';
      case 'STATS_QUERY_FAILED':
      case 'LOG_QUERY_FAILED':
        return 'Aegis could not read dashboard metadata. The gateway is still running; try again in a moment.';
      case 'KEY_QUERY_FAILED':
      case 'KEY_GENERATION_FAILED':
      case 'KEY_CREATE_FAILED':
      case 'KEY_REVOKE_FAILED':
      case 'KEY_ACL_UPDATE_FAILED':
        return 'Aegis could not update API keys. Try again, then check the local terminal log if it repeats.';
      case 'TEMPLATE_QUERY_FAILED':
      case 'TEMPLATE_CREATE_FAILED':
      case 'TEMPLATE_UPDATE_FAILED':
      case 'TEMPLATE_DELETE_FAILED':
        return 'Aegis could not update prompt templates. Try again, then check the local terminal log if it repeats.';
      case 'RATE_LIMITED':
        return 'This API key is being rate limited. Wait a minute or raise the local rate limit in Settings.';
      case 'UNAUTHORIZED':
        return 'API key rejected. Sign in again.';
      default:
        return error.message || 'Request failed';
    }
  }
  if (error instanceof DOMException && error.name === 'AbortError') {
    return 'Generation stopped';
  }
  if (error instanceof Error) {
    return error.message;
  }
  return 'Request failed';
}

export function setAuthToken(token: string): void {
  currentAuthToken = token;
  if (token) {
    client.defaults.headers.common.Authorization = `Bearer ${token}`;
  } else {
    delete client.defaults.headers.common.Authorization;
  }
}

export async function getHardware(): Promise<HardwareInfo> {
  const res = await client.get<HardwareInfo>('/hardware', { timeout: 30000 });
  return res.data;
}

export async function getModels(): Promise<ModelInfo[]> {
  const res = await client.get<ModelListResponse>('/models', { timeout: 30000 });
  return res.data.data;
}

export async function getModelCatalog(category: CatalogCategory, limit: number, offset: number): Promise<ModelCatalogResponse> {
  const res = await client.get<ModelCatalogResponse>('/models/catalog', {
    params: { category, limit, offset },
    timeout: 10000,
  });
  return res.data;
}

export async function pullModel(model: string): Promise<PullJob> {
  const res = await client.post<PullJob>('/models/pull', { model }, { timeout: 15000 });
  return res.data;
}

export async function getPullJob(model: string): Promise<PullJob> {
  const res = await client.get<PullJob>(`/models/pull/${encodeURIComponent(model)}`, { timeout: 10000 });
  return res.data;
}

export async function getStats(): Promise<StatsResponse> {
  const res = await client.get<StatsResponse>('/stats', { timeout: 30000 });
  return res.data;
}

export async function getLogs(limit: number, offset: number): Promise<LogsResponse> {
  const res = await client.get<LogsResponse>('/logs', { params: { limit, offset }, timeout: 30000 });
  return res.data;
}

export async function getKeys(): Promise<APIKey[]> {
  const res = await client.get<KeysResponse>('/keys', { timeout: 30000 });
  return res.data.data;
}

export async function createKey(label: string, allowedModels: string[]): Promise<CreateKeyResponse> {
  const res = await client.post<CreateKeyResponse>('/keys', { label, allowed_models: allowedModels }, { timeout: 30000 });
  return res.data;
}

export async function revokeKey(id: string): Promise<void> {
  await client.delete(`/keys/${encodeURIComponent(id)}`, { timeout: 30000 });
}

export async function updateKeyModels(id: string, allowedModels: string[]): Promise<APIKey[]> {
  const res = await client.patch<KeysResponse>(
    `/keys/${encodeURIComponent(id)}/models`,
    { allowed_models: allowedModels },
    { timeout: 30000 },
  );
  return res.data.data;
}

export async function getTemplates(): Promise<PromptTemplate[]> {
  const res = await client.get<TemplatesResponse>('/templates', { timeout: 30000 });
  return res.data.data;
}

export async function createTemplate(payload: TemplatePayload): Promise<PromptTemplate> {
  const res = await client.post<PromptTemplate>('/templates', payload, { timeout: 30000 });
  return res.data;
}

export async function updateTemplate(id: string, payload: TemplatePayload): Promise<PromptTemplate> {
  const res = await client.patch<PromptTemplate>(`/templates/${encodeURIComponent(id)}`, payload, { timeout: 30000 });
  return res.data;
}

export async function deleteTemplate(id: string): Promise<void> {
  await client.delete(`/templates/${encodeURIComponent(id)}`, { timeout: 30000 });
}

export async function getConfig(): Promise<ConfigResponse> {
  const res = await client.get<ConfigResponse>('/config', { timeout: 30000 });
  return res.data;
}

export async function patchConfig(patch: ConfigPatch): Promise<ConfigResponse> {
  const res = await client.patch<ConfigResponse>('/config', patch, { timeout: 30000 });
  return res.data;
}

export async function sendChatCompletion(model: string, messages: ChatMessage[]): Promise<ChatResult> {
  const body: ChatCompletionRequest = {
    model,
    messages,
    stream: false,
  };
  const res = await client.post<ChatCompletionResponse>('/chat/completions', body, { timeout: 180000 });
  return {
    content: res.data.choices[0]?.message.content ?? '',
    model: res.data.model,
    routedModel: String(res.headers['x-aegis-routed-model'] ?? res.data.model),
    fallback: String(res.headers['x-aegis-fallback'] ?? 'false') === 'true',
    usage: res.data.usage,
  };
}

export async function streamChatCompletion(
  model: string,
  messages: ChatMessage[],
  onToken: (token: string) => void,
  signal?: AbortSignal,
): Promise<ChatResult> {
  const response = await fetch('/v1/chat/completions', {
    method: 'POST',
    signal,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${currentAuthToken}`,
    },
    body: JSON.stringify({
      model,
      messages,
      stream: true,
      stream_options: { include_usage: true },
    }),
  });

  if (!response.ok) {
    throw await readAPIError(response);
  }
  if (!response.body) {
    throw new Error('streaming is unavailable in this browser');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let content = '';
  const promptTokens = estimateMessages(messages);
  let usage: ChatCompletionResponse['usage'] | null = null;

  while (true) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const events = buffer.split('\n\n');
    buffer = events.pop() ?? '';
    for (const event of events) {
      const data = event
        .split('\n')
        .filter((line) => line.startsWith('data:'))
        .map((line) => line.slice(5).trim())
        .join('\n');
      if (!data || data === '[DONE]') {
        continue;
      }
      const chunk = JSON.parse(data) as ChatCompletionChunk | APIErrorResponse;
      if ('error' in chunk) {
        throw new GatewayAPIError(chunk.error, 0, chunk.code || 'BACKEND_STREAM_ERROR');
      }
      const token = chunk.choices[0]?.delta.content ?? '';
      if (token) {
        content += token;
        onToken(token);
      }
      if ('usage' in chunk && chunk.usage) {
        usage = chunk.usage;
      }
    }
  }

  const estimatedCompletion = estimateText(content);
  return {
    content,
    model,
    routedModel: response.headers.get('X-Aegis-Routed-Model') ?? model,
    fallback: response.headers.get('X-Aegis-Fallback') === 'true',
    usage: usage ?? {
      prompt_tokens: promptTokens,
      completion_tokens: estimatedCompletion,
      total_tokens: promptTokens + estimatedCompletion,
    },
  };
}

function estimateMessages(messages: ChatMessage[]): number {
  return messages.reduce((total, message) => total + estimateText(message.role) + estimateText(message.content), 0);
}

function estimateText(value: string): number {
  const trimmed = value.trim();
  if (!trimmed) {
    return 0;
  }
  return Math.max(1, Math.ceil(Array.from(trimmed).length / 4));
}

async function readAPIError(response: Response): Promise<GatewayAPIError> {
  try {
    const body = (await response.json()) as APIErrorResponse;
    return new GatewayAPIError(
      body.error || `request failed with status ${response.status}`,
      response.status,
      body.code || 'REQUEST_FAILED',
    );
  } catch {
    return new GatewayAPIError(`request failed with status ${response.status}`, response.status, 'REQUEST_FAILED');
  }
}
