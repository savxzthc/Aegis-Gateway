# Changelog

## Unreleased

- Added auth hot-path caching to avoid repeated PBKDF2 work for every active key on every request.
- Added cached active-key secret reads with invalidation on key create and revoke.
- Added background rate-limiter pruning to prevent stale key growth.
- Added connection reuse fixes for Ollama lifecycle HTTP responses.
- Added `:latest`-aware Ollama model matching.
- Added bounded HTTP write timeout for long-running streams.
- Added single pull-job status endpoint at `/v1/models/pull/{model}`.
- Added clearer startup backend output and LAN binding warnings.
- Added token-cache eviction, request log totals, explicit frontend API timeouts, and llama.cpp backend refresh on config updates.
- Hardened release metadata, completion finish reasons, log pagination, lifecycle load waiting, and unsupported multi-completion validation.
- Added SSE shutdown cancellation, stricter auth-failure throttling, atomic config rewrites, JSON content lengths, stream usage support, and startup backend reachability warnings.
- Added per-key model allowlists and a SQLite-backed prompt template library with dashboard controls.

## v0.1.0

- Initial Aegis Gateway foundation with OpenAI-compatible chat and completions APIs.
- Added local dashboard, API key management, VRAM-aware routing, model lifecycle control, metadata-only audit logs, and downloadable Ollama model catalog.
