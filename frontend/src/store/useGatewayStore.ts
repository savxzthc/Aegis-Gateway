import { create } from 'zustand';
import {
  APIKey,
  CatalogCategory,
  CatalogModel,
  ConfigPatch,
  ConfigResponse,
  CreateKeyResponse,
  GatewayAPIError,
  HardwareInfo,
  LogsResponse,
  ModelInfo,
  PromptTemplate,
  RequestLog,
  StatsResponse,
  TemplatePayload,
  createKey,
  createTemplate,
  deleteTemplate,
  gatewayErrorMessage,
  getConfig,
  getHardware,
  getModelCatalog,
  getKeys,
  getLogs,
  getModels,
  getStats,
  getTemplates,
  patchConfig,
  pullModel,
  revokeKey,
  setAuthToken,
  updateKeyModels,
  updateTemplate,
} from '../api/client';

const storedToken = window.localStorage.getItem('aegis_api_key') ?? '';
setAuthToken(storedToken);

interface GatewayState {
  token: string;
  connected: boolean;
  error: string;
  hardware: HardwareInfo | null;
  models: ModelInfo[];
  catalogOnline: boolean;
  modelCatalog: CatalogModel[];
  catalogCategory: CatalogCategory;
  catalogTotal: number;
  catalogLimit: number;
  catalogOffset: number;
  stats: StatsResponse | null;
  logs: RequestLog[];
  logLimit: number;
  logOffset: number;
  logTotal: number;
  keys: APIKey[];
  templates: PromptTemplate[];
  config: ConfigResponse | null;
  createdKey: CreateKeyResponse | null;
  setToken: (token: string) => void;
  clearToken: () => void;
  clearCreatedKey: () => void;
  loadHardware: () => Promise<void>;
  loadModels: () => Promise<void>;
  loadModelCatalog: (category?: CatalogCategory, append?: boolean, limit?: number) => Promise<void>;
  pullCatalogModel: (model: string) => Promise<void>;
  loadStats: () => Promise<void>;
  loadLogs: (offset?: number) => Promise<void>;
  loadKeys: () => Promise<void>;
  loadTemplates: () => Promise<void>;
  loadConfig: () => Promise<void>;
  createAPIKey: (label: string, allowedModels?: string[]) => Promise<void>;
  revokeAPIKey: (id: string) => Promise<void>;
  saveKeyModels: (id: string, allowedModels: string[]) => Promise<void>;
  createPromptTemplate: (payload: TemplatePayload) => Promise<void>;
  updatePromptTemplate: (id: string, payload: TemplatePayload) => Promise<void>;
  deletePromptTemplate: (id: string) => Promise<void>;
  saveConfig: (patch: ConfigPatch) => Promise<void>;
}

export const useGatewayStore = create<GatewayState>((set, get) => ({
  token: storedToken,
  connected: false,
  error: '',
  hardware: null,
  models: [],
  catalogOnline: false,
  modelCatalog: [],
  catalogCategory: 'standard',
  catalogTotal: 0,
  catalogLimit: 12,
  catalogOffset: 0,
  stats: null,
  logs: [],
  logLimit: 50,
  logOffset: 0,
  logTotal: 0,
  keys: [],
  templates: [],
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
  loadModelCatalog: async (category, append = false, limit) => {
    await guard(set, async () => {
      const selectedCategory = category ?? get().catalogCategory;
      const offset = append ? get().modelCatalog.length : 0;
      const selectedLimit = limit ?? get().catalogLimit;
      const catalog = await getModelCatalog(selectedCategory, selectedLimit, offset);
      set({
        modelCatalog: append ? [...get().modelCatalog, ...catalog.data] : catalog.data,
        catalogOnline: catalog.online,
        catalogCategory: catalog.category,
        catalogTotal: catalog.total,
        catalogLimit: catalog.limit,
        catalogOffset: catalog.offset,
        connected: true,
      });
    });
  },
  pullCatalogModel: async (model) => {
    try {
      await pullModel(model);
      const catalog = await getModelCatalog(get().catalogCategory, Math.max(get().catalogLimit, get().modelCatalog.length), 0);
      const models = await getModels();
      set({
        modelCatalog: catalog.data,
        catalogOnline: catalog.online,
        catalogTotal: catalog.total,
        catalogLimit: catalog.limit,
        catalogOffset: catalog.offset,
        models,
        connected: true,
      });
    } catch (error) {
      if (error instanceof GatewayAPIError && error.status === 401) {
        window.localStorage.removeItem('aegis_api_key');
        setAuthToken('');
        set({ token: '', connected: false, error: 'API key rejected. Sign in again.' });
      } else {
        set({ error: errorMessage(error), connected: false });
      }
      throw error;
    }
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
      set({ logs: res.data, logLimit: res.limit, logOffset: res.offset, logTotal: res.total, connected: true });
    });
  },
  loadKeys: async () => {
    await guard(set, async () => {
      const keys = await getKeys();
      set({ keys, connected: true });
    });
  },
  loadTemplates: async () => {
    await guard(set, async () => {
      const templates = await getTemplates();
      set({ templates, connected: true });
    });
  },
  loadConfig: async () => {
    await guard(set, async () => {
      const config = await getConfig();
      set({ config, connected: true });
    });
  },
  createAPIKey: async (label, allowedModels = []) => {
    await guard(set, async () => {
      const createdKey = await createKey(label, allowedModels);
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
  saveKeyModels: async (id, allowedModels) => {
    await guard(set, async () => {
      const keys = await updateKeyModels(id, allowedModels);
      set({ keys, connected: true });
    });
  },
  createPromptTemplate: async (payload) => {
    await guard(set, async () => {
      await createTemplate(payload);
      const templates = await getTemplates();
      set({ templates, connected: true });
    });
  },
  updatePromptTemplate: async (id, payload) => {
    await guard(set, async () => {
      await updateTemplate(id, payload);
      const templates = await getTemplates();
      set({ templates, connected: true });
    });
  },
  deletePromptTemplate: async (id) => {
    await guard(set, async () => {
      await deleteTemplate(id);
      const templates = await getTemplates();
      set({ templates, connected: true });
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
