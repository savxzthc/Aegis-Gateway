# Aegis Gateway

`[ AEGIS GATEWAY ]` Privacy-first local AI control plane for consumer GPUs.

## What is Aegis?

Aegis Gateway is a local-only API gateway that sits between your applications and local model runners such as Ollama or llama.cpp. It exposes OpenAI-compatible REST endpoints, adds API keys, rate limits, audit metadata, model routing, and a dashboard without sending prompts, logs, metrics, or telemetry anywhere.

It is built for people running real models on real desktop hardware. If your machine has an RTX 4060 or 4070 and you want larger models when VRAM allows, smaller fallbacks when it does not, automatic unloading when you go idle, and a browser chat UI that does not require writing code, Aegis gives you those controls in one binary.

## Why not just use Ollama / LiteLLM?

| Feature | Ollama alone | LiteLLM | Aegis Gateway |
| --- | --- | --- | --- |
| VRAM-aware model routing | No | No | Yes |
| On-demand VRAM unloading | No | No | Yes |
| Built-in web dashboard | No | Partial | Yes |
| In-browser chat UI | No | Partial | Yes |
| OpenAI-compatible API | Yes | Yes | Yes |
| Single binary, no Docker | Yes | No | Yes |
| Zero telemetry | Yes | Partial | Yes |
| Consumer GPU target | Partial | No | Yes |
| Supply-chain minimal deps | N/A | No | Yes |

## Quick Start

```bash
git clone https://github.com/savxzthc/aegis-gateway
cd aegis-gateway
make build
./aegis-gateway
```

On Windows without `make`, build the same way with:

```powershell
cd frontend
npm ci
npm run build
cd ..
go build -o aegis-gateway.exe .
.\aegis-gateway.exe
```

On first boot, Aegis prints one API key. Save it somewhere local; the raw key is never stored or printed again. Open the `dashboard:` URL printed in the terminal, paste the key, and use the Chat view to talk to registered models without writing code. The default dashboard URL is `http://127.0.0.1:9000`.

If Ollama is configured to use port `9000`, Aegis will move its dashboard to the next free local port and print a warning plus the new URL. In that case, keep Ollama running and open the Aegis `dashboard:` URL from the banner instead of `http://127.0.0.1:9000`.

## Local Chat

The dashboard opens to Chat after login. Pick a model from the right rail, type a message, and press Enter to send. Responses stream token-by-token through Aegis, and the message footer shows the routed model plus a fallback marker if VRAM routing changed the requested model.

Chat messages are held in browser memory only. They are not written to SQLite, request logs, or any external service.

Ollama must be running when you chat with Ollama-backed models. If your Ollama install uses a nonstandard port through `OLLAMA_HOST`, Aegis uses that value automatically when its own `ollama_base_url` is still the default.

Audit records are metadata-only and are automatically pruned after 30 days. Failed authentication attempt records are pruned after 7 days.

## API Reference

Browser apps running from `localhost`, `127.0.0.1`, or `[::1]` can call `/v1` with Bearer auth. Other browser origins are rejected by CORS before authentication.

Every API response includes `X-Request-Id` and `X-Aegis-Version` so local clients can correlate failures without telemetry.

| Endpoint | Method | Auth | Description |
| --- | --- | --- | --- |
| `/v1/chat/completions` | POST | Yes | OpenAI-compatible chat completions with streaming support |
| `/v1/completions` | POST | Yes | Legacy completions compatibility endpoint |
| `/v1/models` | GET | Yes | OpenAI-style model list plus Aegis metadata |
| `/v1/hardware` | GET | Yes | Current GPU and VRAM state |
| `/v1/stats` | GET | Yes | Request totals, latency, model usage, fallback rate, hourly counts |
| `/v1/logs?limit=50&offset=0` | GET | Yes | Metadata-only audit log |
| `/v1/keys` | GET | Yes | List active API keys |
| `/v1/keys` | POST | Yes | Create a new API key |
| `/v1/keys/{id}` | DELETE | Yes | Revoke an API key |
| `/v1/config` | GET | Yes | Read editable and runtime configuration |
| `/v1/config` | PATCH | Yes | Update editable configuration values |
| `/healthz` | GET | No | Local process health check |
| `/readyz` | GET | No | Readiness check covering SQLite and embedded frontend assets |

## config.toml Reference

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `server.host` | string | `0.0.0.0` | Address the gateway binds to |
| `server.port` | integer | `9000` | Port for API and dashboard; restart required after changing |
| `server.idle_timeout_minutes` | integer | `10` | Minutes before an idle Ollama model is unloaded |
| `server.request_timeout_seconds` | integer | `300` | Maximum seconds a model request may run before cancellation |
| `security.rate_limit_rpm` | integer | `60` | Requests per minute per API key, or `0` for unlimited |
| `security.auto_generate_key` | boolean | `true` | Generate and print an initial API key when the DB has no active keys |
| `backend.default_type` | string | `ollama` | Default backend for models without an override |
| `backend.ollama_base_url` | string | `http://127.0.0.1:11434` | Ollama HTTP API base URL |
| `backend.llamacpp_base_url` | string | `http://127.0.0.1:8080` | llama.cpp server base URL |
| `models.registry.<name>.vram_gb` | number | varies | Estimated VRAM needed to run the model |
| `models.registry.<name>.backend` | string | `ollama` | Backend override for the model |
| `models.registry.<name>.description` | string | varies | Human-readable dashboard description |

## How VRAM Routing Works

When a request asks for a model, Aegis checks the current free VRAM and the model registry. If the requested model fits, it runs that model. If it does not fit, Aegis chooses the largest registered model that does fit and marks the response with `X-Aegis-Fallback: true`.

For example, if `llama3:8b` needs about 5.5 GB and your GPU currently has 3.2 GB free, Aegis can route the request to `phi3:mini` instead. The client still receives an OpenAI-compatible response, and the audit log records only metadata: requested model, used model, latency, status, and token estimates.

## Verification

Production builds should pass these checks:

```bash
cd frontend && npm ci && npm run build
cd ..
go test ./...
go test -race ./...
go vet ./...
go build -o aegis-gateway .
```

`npm audit --omit=dev` should report zero production dependency vulnerabilities. Vite is a development-only dependency; keep `frontend:dev` bound to `127.0.0.1`.

## Roadmap

- [ ] Discord bot integration
- [ ] Windows system tray launcher
- [ ] Multi-GPU support
- [ ] Per-key model ACLs (whitelist which models a key can access)
- [ ] Prompt template library (stored in SQLite, selectable per request)

## License

MIT

See [LICENSE](LICENSE).

## Third-Party Licenses

This project uses third-party open-source dependencies. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) for a summary of dependency licenses.

If you distribute compiled binaries or bundled frontend assets, include the relevant third-party license notices with the release.

Built by @savxz
