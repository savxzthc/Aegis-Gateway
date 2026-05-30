import { create } from 'zustand';
import {
  APIKey,
  ConfigPatch,
  ConfigResponse,
  CreateKeyResponse,
  GatewayAPIError,
  HardwareInfo,
  LogsResponse,
  ModelInfo,
  RequestLog,
  StatsResponse,
  createKey,
  gatewayErrorMessage,
  getConfig,
  getHardware,
  getKeys,
  getLogs,
  getModels,
  getStats,
  patchConfig,
  revokeKey,
  setAuthToken,
} from '../api/client';

const storedToken = window.localStorage.getItem('aegis_api_key') ?? '';
setAuthToken(storedToken);

interface GatewayState {
  token: string;
  connected: boolean;
  error: string;
  hardware: HardwareInfo | null;
  models: ModelInfo[];
  stats: StatsResponse | null;
  logs: RequestLog[];
  logLimit: number;
  logOffset: number;
  keys: APIKey[];
  config: ConfigResponse | null;
  createdKey: CreateKeyResponse | null;
  setToken: (token: string) => void;
  clearToken: () => void;
  clearCreatedKey: () => void;
  loadHardware: () => Promise<void>;
  loadModels: () => Promise<void>;
  loadStats: () => Promise<void>;
  loadLogs: (offset?: number) => Promise<void>;
  loadKeys: () => Promise<void>;
  loadConfig: () => Promise<void>;
  createAPIKey: (label: string) => Promise<void>;
  revokeAPIKey: (id: string) => Promise<void>;
  saveConfig: (patch: ConfigPatch) => Promise<void>;
}

export const useGatewayStore = create<GatewayState>((set, get) => ({
  token: storedToken,
  connected: false,
  error: '',
  hardware: null,
  models: [],
  stats: null,
  logs: [],
  logLimit: 50,
  logOffset: 0,
  keys: [],
  config: null,
  createdKey: null,
  setToken: (token) => {
    window.localStorage.setItem('aegis_api_key', token);
    setAuthToken(token);
    set({ token, error: '' });
  },
  clearToken: () => {
    window.localStorage.removeItem('aegis_api_key');
    setAuthToken('');
    set({ token: '', connected: false });
  },
  clearCreatedKey: () => set({ createdKey: null }),
  loadHardware: async () => {
    await guard(set, async () => {
      const hardware = await getHardware();
      set({ hardware, connected: true });
    });
  },
  loadModels: async () => {
    await guard(set, async () => {
      const models = await getModels();
      set({ models, connected: true });
    });
  },
  loadStats: async () => {
    await guard(set, async () => {
      const stats = await getStats();
      set({ stats, connected: true });
    });
  },
  loadLogs: async (offset) => {
    await guard(set, async () => {
      const nextOffset = offset ?? get().logOffset;
      const res: LogsResponse = await getLogs(get().logLimit, nextOffset);
      set({ logs: res.data, logLimit: res.limit, logOffset: res.offset, connected: true });
    });
  },
  loadKeys: async () => {
    await guard(set, async () => {
      const keys = await getKeys();
      set({ keys, connected: true });
    });
  },
  loadConfig: async () => {
    await guard(set, async () => {
      const config = await getConfig();
      set({ config, connected: true });
    });
  },
  createAPIKey: async (label) => {
    await guard(set, async () => {
      const createdKey = await createKey(label);
      const keys = await getKeys();
      set({ createdKey, keys, connected: true });
    });
  },
  revokeAPIKey: async (id) => {
    await guard(set, async () => {
      await revokeKey(id);
      const keys = await getKeys();
      set({ keys, connected: true });
    });
  },
  saveConfig: async (patch) => {
    await guard(set, async () => {
      const config = await patchConfig(patch);
      set({ config, connected: true });
    });
  },
}));

async function guard(
  set: (partial: Partial<GatewayState>) => void,
  fn: () => Promise<void>,
): Promise<void> {
  try {
    await fn();
    set({ error: '' });
  } catch (error) {
    if (error instanceof GatewayAPIError && error.status === 401) {
      window.localStorage.removeItem('aegis_api_key');
      setAuthToken('');
      set({ token: '', connected: false, error: 'API key rejected. Sign in again.' });
      return;
    }
    set({ error: errorMessage(error), connected: false });
  }
}

function errorMessage(error: unknown): string {
  return gatewayErrorMessage(error);
}
