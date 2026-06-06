package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const (
	requestLogRetention  = 30 * 24 * time.Hour
	authFailureRetention = 7 * 24 * time.Hour
	metadataPruneEvery   = time.Hour
	activeKeysCacheTTL   = 15 * time.Second
	sqliteTimeLayout     = "2006-01-02T15:04:05.000000000Z"
)

// APIKeySecret contains stored hash material for active API keys.
type APIKeySecret struct {
	ID              string
	Salt            string
	Hash            string
	KeyRole         string
	RateLimitRPM    int
	MaxPromptTokens int
	AllowedIPs      string
	OwnerID         string
}

// NewAPIKey contains fields required to store a generated key.
type NewAPIKey struct {
	ID        string
	Label     string
	Salt      string
	Hash      string
	KeyRole   string
	OwnerID   string
	CreatedAt time.Time
}

// APIKeyView is the dashboard-safe representation of an API key.
type APIKeyView struct {
	ID              string     `json:"id"`
	Label           string     `json:"label"`
	KeyRole         string     `json:"key_role"`
	OwnerID         *string    `json:"owner_id,omitempty"`
	RateLimitRPM    int        `json:"rate_limit_rpm"`
	MaxPromptTokens int        `json:"max_prompt_tokens"`
	AllowedIPs      []string   `json:"allowed_ips"`
	CreatedAt       time.Time  `json:"created_at"`
	LastUsed        *time.Time `json:"last_used"`
	RequestsTotal   int64      `json:"requests_total"`
	AllowedModels   []string   `json:"allowed_models"`
}

// RequestLog contains privacy-preserving request metadata.
type RequestLog struct {
	ID                        int64     `json:"id"`
	Timestamp                 time.Time `json:"timestamp"`
	KeyID                     string    `json:"key_id"`
	ModelRequested            string    `json:"model_requested"`
	ModelUsed                 string    `json:"model_used"`
	FallbackTriggered         bool      `json:"fallback_triggered"`
	BackendType               string    `json:"backend_type"`
	LatencyMS                 int64     `json:"latency_ms"`
	EstimatedPromptTokens     int       `json:"estimated_prompt_tokens"`
	EstimatedCompletionTokens int       `json:"estimated_completion_tokens"`
	StatusCode                int       `json:"status_code"`
	TokensPerSecond           float64   `json:"tokens_per_second"`
}

// TopModelStat contains an aggregate request count by model.
type TopModelStat struct {
	Model string `json:"model"`
	Count int64  `json:"count"`
}

// HourlyRequestStat contains an hourly request count.
type HourlyRequestStat struct {
	Hour  string `json:"hour"`
	Count int64  `json:"count"`
}

// Stats contains aggregate request metadata for the dashboard.
type Stats struct {
	RequestsToday     int64               `json:"requests_today"`
	RequestsYesterday int64               `json:"requests_yesterday"`
	RequestsTotal     int64               `json:"requests_total"`
	AvgLatencyMS      int64               `json:"avg_latency_ms"`
	TopModels         []TopModelStat      `json:"top_models"`
	FallbackRatePct   float64             `json:"fallback_rate_pct"`
	RequestsPerHour   []HourlyRequestStat `json:"requests_per_hour"`
}

// PromptTemplate contains a reusable local prompt stored in SQLite.
type PromptTemplate struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SystemPrompt string    `json:"system_prompt"`
	Prompt       string    `json:"prompt"`
	Model        string    `json:"model"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CountActiveKeys returns the number of non-revoked API keys.
func (s *Store) CountActiveKeys(ctx context.Context) (int, error) {
	var count int
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`).Scan(&count)
	return count, err
}

// HealthCheck verifies that SQLite is reachable and responding.
func (s *Store) HealthCheck(ctx context.Context) error {
	var value int
	return s.conn.QueryRowContext(ctx, `SELECT 1`).Scan(&value)
}

// CreateAPIKey stores a new hashed API key.
func (s *Store) CreateAPIKey(ctx context.Context, key NewAPIKey) error {
	role := key.KeyRole
	if role == "" {
		role = "inference"
	}
	var ownerID interface{} = nil
	if key.OwnerID != "" {
		ownerID = key.OwnerID
	}
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO api_keys (id, label, salt, hash, created_at, requests_total, key_role, owner_id)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, key.ID, key.Label, key.Salt, key.Hash, formatTime(key.CreatedAt), role, ownerID)
	if err == nil {
		s.invalidateActiveKeySecrets()
	}
	return err
}

// UpdateKeyRateLimit sets the per-key RPM override. 0 = inherit global.
func (s *Store) UpdateKeyRateLimit(ctx context.Context, id string, rpm int) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `UPDATE api_keys SET rate_limit_rpm = ? WHERE id = ? AND revoked_at IS NULL`, rpm, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ResetAPIKeys revokes every active API key and stores one replacement key.
func (s *Store) ResetAPIKeys(ctx context.Context, key NewAPIKey, at time.Time) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `
		UPDATE api_keys
		SET revoked_at = ?
		WHERE revoked_at IS NULL
	`, formatTime(at)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO api_keys (id, label, salt, hash, created_at, requests_total)
		VALUES (?, ?, ?, ?, ?, 0)
	`, key.ID, key.Label, key.Salt, key.Hash, formatTime(key.CreatedAt)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.invalidateActiveKeySecrets()
	return nil
}

// ActiveKeySecrets returns hash material for all active keys.
func (s *Store) ActiveKeySecrets(ctx context.Context) ([]APIKeySecret, error) {
	now := time.Now().UTC()
	s.activeKeysMu.RLock()
	if now.Before(s.activeKeysExpires) {
		keys := cloneAPIKeySecrets(s.activeKeys)
		s.activeKeysMu.RUnlock()
		return keys, nil
	}
	s.activeKeysMu.RUnlock()

	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, salt, hash, COALESCE(key_role,'inference'), COALESCE(rate_limit_rpm,0), COALESCE(max_prompt_tokens,0), COALESCE(allowed_ips,''), COALESCE(owner_id,'')
		FROM api_keys
		WHERE revoked_at IS NULL
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKeySecret
	for rows.Next() {
		var key APIKeySecret
		if err := rows.Scan(&key.ID, &key.Salt, &key.Hash, &key.KeyRole, &key.RateLimitRPM, &key.MaxPromptTokens, &key.AllowedIPs, &key.OwnerID); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.activeKeysMu.Lock()
	s.activeKeys = cloneAPIKeySecrets(keys)
	s.activeKeysExpires = now.Add(activeKeysCacheTTL)
	s.activeKeysMu.Unlock()
	return keys, nil
}

// ListAPIKeys returns dashboard-safe active API keys.
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKeyView, error) {
	return s.listAPIKeysFiltered(ctx, "", "")
}

// ListAPIKeysByOwner returns active API keys owned by a specific user.
func (s *Store) ListAPIKeysByOwner(ctx context.Context, ownerID string) ([]APIKeyView, error) {
	return s.listAPIKeysFiltered(ctx, ownerID, "")
}

func (s *Store) listAPIKeysFiltered(ctx context.Context, ownerID, keyRole string) ([]APIKeyView, error) {
	query := `SELECT id, label, key_role, owner_id, rate_limit_rpm, max_prompt_tokens, allowed_ips, created_at, last_used, requests_total
		FROM api_keys WHERE revoked_at IS NULL`
	var args []interface{}
	if ownerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, ownerID)
	}
	if keyRole != "" {
		query += ` AND key_role = ?`
		args = append(args, keyRole)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKeyView
	for rows.Next() {
		var key APIKeyView
		var created string
		var last sql.NullString
		var ownerIDNull sql.NullString
		var allowedIPsNull sql.NullString
		if err := rows.Scan(&key.ID, &key.Label, &key.KeyRole, &ownerIDNull, &key.RateLimitRPM, &key.MaxPromptTokens, &allowedIPsNull, &created, &last, &key.RequestsTotal); err != nil {
			return nil, err
		}
		createdAt, err := parseTime(created)
		if err != nil {
			return nil, err
		}
		key.CreatedAt = createdAt
		if last.Valid {
			lastUsed, err := parseTime(last.String)
			if err != nil {
				return nil, err
			}
			key.LastUsed = &lastUsed
		}
		if ownerIDNull.Valid {
			key.OwnerID = &ownerIDNull.String
		}
		if allowedIPsNull.Valid && allowedIPsNull.String != "" {
			key.AllowedIPs = splitCIDRs(allowedIPsNull.String)
		}
		if key.AllowedIPs == nil {
			key.AllowedIPs = []string{}
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range keys {
		models, err := s.APIKeyAllowedModels(ctx, keys[i].ID)
		if err != nil {
			return nil, err
		}
		if models == nil {
			models = []string{}
		}
		keys[i].AllowedModels = models
	}
	return keys, nil
}

func splitCIDRs(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}

// APIKeyAllowedModels returns the model allowlist for a key. An empty list means all registered models are allowed.
func (s *Store) APIKeyAllowedModels(ctx context.Context, id string) ([]string, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT model_id
		FROM api_key_model_acls
		WHERE key_id = ?
		ORDER BY model_id ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []string
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if models == nil {
		models = []string{}
	}
	return models, nil
}

// SetAPIKeyAllowedModels replaces the model allowlist for a key. An empty list allows all models.
func (s *Store) SetAPIKeyAllowedModels(ctx context.Context, id string, models []string, at time.Time) (bool, error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE id = ? AND revoked_at IS NULL`, id).Scan(&active); err != nil {
		return false, err
	}
	if active == 0 {
		if err = tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM api_key_model_acls WHERE key_id = ?`, id); err != nil {
		return false, err
	}
	for _, model := range models {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api_key_model_acls (key_id, model_id, created_at)
			VALUES (?, ?, ?)
		`, id, model, formatTime(at)); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// KeyAllowsModel reports whether a key may use model. Keys with no explicit ACL allow every model.
func (s *Store) KeyAllowsModel(ctx context.Context, id, model string) (bool, error) {
	var total int
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_key_model_acls WHERE key_id = ?`, id).Scan(&total); err != nil {
		return false, err
	}
	if total == 0 {
		return true, nil
	}
	var allowed int
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_key_model_acls WHERE key_id = ? AND model_id = ?`, id, model).Scan(&allowed)
	return allowed > 0, err
}

// MarkKeyUsed records successful key usage.
func (s *Store) MarkKeyUsed(ctx context.Context, id string, at time.Time) error {
	_, err := s.conn.ExecContext(ctx, `
		UPDATE api_keys
		SET last_used = ?, requests_total = requests_total + 1
		WHERE id = ? AND revoked_at IS NULL
	`, formatTime(at), id)
	return err
}

// RevokeAPIKey revokes an active API key.
func (s *Store) RevokeAPIKey(ctx context.Context, id string, at time.Time) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `
		UPDATE api_keys
		SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, formatTime(at), id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err == nil && affected > 0 {
		s.invalidateActiveKeySecrets()
	}
	return affected > 0, err
}

// RevokeAPIKeyIfNotLast revokes an active key unless it is the final active key.
func (s *Store) RevokeAPIKeyIfNotLast(ctx context.Context, id string, at time.Time) (revoked bool, lastActive bool, err error) {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var targetActive int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE id = ? AND revoked_at IS NULL`, id).Scan(&targetActive); err != nil {
		return false, false, err
	}
	if targetActive == 0 {
		if err = tx.Commit(); err != nil {
			return false, false, err
		}
		return false, false, nil
	}

	var activeCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_keys WHERE revoked_at IS NULL`).Scan(&activeCount); err != nil {
		return false, false, err
	}
	if activeCount <= 1 {
		if err = tx.Commit(); err != nil {
			return false, false, err
		}
		return false, true, nil
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys
		SET revoked_at = ?
		WHERE id = ? AND revoked_at IS NULL
	`, formatTime(at), id)
	if err != nil {
		return false, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, false, err
	}
	if err = tx.Commit(); err != nil {
		return false, false, err
	}
	if affected > 0 {
		s.invalidateActiveKeySecrets()
	}
	return affected > 0, false, nil
}

// ListPromptTemplates returns all reusable prompt templates.
func (s *Store) ListPromptTemplates(ctx context.Context) ([]PromptTemplate, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, name, system_prompt, prompt, model, created_at, updated_at
		FROM prompt_templates
		ORDER BY updated_at DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var templates []PromptTemplate
	for rows.Next() {
		template, err := scanPromptTemplate(rows)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, rows.Err()
}

// CreatePromptTemplate stores a new prompt template.
func (s *Store) CreatePromptTemplate(ctx context.Context, template PromptTemplate) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO prompt_templates (id, name, system_prompt, prompt, model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, template.ID, template.Name, template.SystemPrompt, template.Prompt, template.Model, formatTime(template.CreatedAt), formatTime(template.UpdatedAt))
	return err
}

// UpdatePromptTemplate replaces editable prompt template fields.
func (s *Store) UpdatePromptTemplate(ctx context.Context, template PromptTemplate) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `
		UPDATE prompt_templates
		SET name = ?, system_prompt = ?, prompt = ?, model = ?, updated_at = ?
		WHERE id = ?
	`, template.Name, template.SystemPrompt, template.Prompt, template.Model, formatTime(template.UpdatedAt), template.ID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// DeletePromptTemplate removes one prompt template.
func (s *Store) DeletePromptTemplate(ctx context.Context, id string) (bool, error) {
	res, err := s.conn.ExecContext(ctx, `DELETE FROM prompt_templates WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// LogAuthFailure records an unauthorized attempt without key material.
func (s *Store) LogAuthFailure(ctx context.Context, ip string, at time.Time) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO auth_failures (ip, timestamp)
		VALUES (?, ?)
	`, ip, formatTime(at))
	if err != nil {
		return err
	}
	return s.maybePruneOldMetadata(ctx, time.Now().UTC())
}

// InsertRequestLog stores privacy-preserving request metadata.
func (s *Store) InsertRequestLog(ctx context.Context, log RequestLog) error {
	_, err := s.conn.ExecContext(ctx, `
		INSERT INTO request_logs (
			timestamp, key_id, model_requested, model_used, fallback_triggered,
			backend_type, latency_ms, estimated_prompt_tokens,
			estimated_completion_tokens, status_code
			, tokens_per_second
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, formatTime(log.Timestamp), log.KeyID, log.ModelRequested, log.ModelUsed, boolInt(log.FallbackTriggered), log.BackendType, log.LatencyMS, log.EstimatedPromptTokens, log.EstimatedCompletionTokens, log.StatusCode, log.TokensPerSecond)
	if err != nil {
		return err
	}
	return s.maybePruneOldMetadata(ctx, time.Now().UTC())
}

// PruneOldMetadata deletes expired audit metadata and auth failures.
func (s *Store) PruneOldMetadata(ctx context.Context, now time.Time) error {
	requestCutoff := now.UTC().Add(-requestLogRetention)
	authCutoff := now.UTC().Add(-authFailureRetention)
	if _, err := s.conn.ExecContext(ctx, `DELETE FROM request_logs WHERE timestamp < ?`, formatTime(requestCutoff)); err != nil {
		return err
	}
	if _, err := s.conn.ExecContext(ctx, `DELETE FROM auth_failures WHERE timestamp < ?`, formatTime(authCutoff)); err != nil {
		return err
	}
	return nil
}

func (s *Store) maybePruneOldMetadata(ctx context.Context, now time.Time) error {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()
	if !s.lastMetadataPrune.IsZero() && now.Sub(s.lastMetadataPrune) < metadataPruneEvery {
		return nil
	}
	if err := s.PruneOldMetadata(ctx, now); err != nil {
		return err
	}
	s.lastMetadataPrune = now
	return nil
}

// ListRequestLogs returns paginated request metadata and the total row count.
func (s *Store) ListRequestLogs(ctx context.Context, limit, offset int) ([]RequestLog, int64, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT id, timestamp, key_id, model_requested, model_used, fallback_triggered,
			backend_type, latency_ms, estimated_prompt_tokens, estimated_completion_tokens, status_code,
			tokens_per_second,
			COUNT(*) OVER () AS total
		FROM request_logs
		ORDER BY timestamp DESC, id DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []RequestLog
	var total int64
	for rows.Next() {
		var log RequestLog
		var ts string
		var fallback int
		if err := rows.Scan(&log.ID, &ts, &log.KeyID, &log.ModelRequested, &log.ModelUsed, &fallback, &log.BackendType, &log.LatencyMS, &log.EstimatedPromptTokens, &log.EstimatedCompletionTokens, &log.StatusCode, &log.TokensPerSecond, &total); err != nil {
			return nil, 0, err
		}
		parsed, err := parseTime(ts)
		if err != nil {
			return nil, 0, err
		}
		log.Timestamp = parsed
		log.FallbackTriggered = fallback == 1
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(logs) == 0 && offset > 0 {
		count, err := s.CountRequestLogs(ctx)
		if err != nil {
			return nil, 0, err
		}
		total = count
	}
	return logs, total, nil
}

// CountRequestLogs returns the total number of request log rows.
func (s *Store) CountRequestLogs(ctx context.Context) (int64, error) {
	var total int64
	err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs`).Scan(&total)
	return total, err
}

// GetStats returns aggregate dashboard statistics.
func (s *Store) GetStats(ctx context.Context, now time.Time) (Stats, error) {
	var stats Stats
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startYesterday := startToday.Add(-24 * time.Hour)
	startTomorrow := startToday.Add(24 * time.Hour)

	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs WHERE timestamp >= ? AND timestamp < ?`, formatTime(startToday), formatTime(startTomorrow)).Scan(&stats.RequestsToday); err != nil {
		return stats, err
	}
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs WHERE timestamp >= ? AND timestamp < ?`, formatTime(startYesterday), formatTime(startToday)).Scan(&stats.RequestsYesterday); err != nil {
		return stats, err
	}
	var avgLatency float64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(AVG(latency_ms), 0) FROM request_logs`).Scan(&stats.RequestsTotal, &avgLatency); err != nil {
		return stats, err
	}
	stats.AvgLatencyMS = int64(avgLatency)
	var fallbackCount int64
	if err := s.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_logs WHERE fallback_triggered = 1`).Scan(&fallbackCount); err != nil {
		return stats, err
	}
	if stats.RequestsTotal > 0 {
		stats.FallbackRatePct = float64(fallbackCount) / float64(stats.RequestsTotal) * 100
	}

	top, err := s.topModels(ctx)
	if err != nil {
		return stats, err
	}
	stats.TopModels = top

	hourly, err := s.hourlyRequests(ctx, now)
	if err != nil {
		return stats, err
	}
	stats.RequestsPerHour = hourly
	return stats, nil
}

func (s *Store) topModels(ctx context.Context) ([]TopModelStat, error) {
	rows, err := s.conn.QueryContext(ctx, `
		SELECT model_used, COUNT(*) AS count
		FROM request_logs
		GROUP BY model_used
		ORDER BY count DESC, model_used ASC
		LIMIT 5
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []TopModelStat
	for rows.Next() {
		var item TopModelStat
		if err := rows.Scan(&item.Model, &item.Count); err != nil {
			return nil, err
		}
		stats = append(stats, item)
	}
	return stats, rows.Err()
}

func (s *Store) hourlyRequests(ctx context.Context, now time.Time) ([]HourlyRequestStat, error) {
	start := now.UTC().Truncate(time.Hour).Add(-23 * time.Hour)
	rows, err := s.conn.QueryContext(ctx, `
		SELECT strftime('%Y-%m-%dT%H:00:00Z', timestamp) AS hour, COUNT(*)
		FROM request_logs
		WHERE timestamp >= ?
		GROUP BY hour
	`, formatTime(start))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int64{}
	for rows.Next() {
		var hour string
		var count int64
		if err := rows.Scan(&hour, &count); err != nil {
			return nil, err
		}
		counts[hour] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]HourlyRequestStat, 0, 24)
	for i := 0; i < 24; i++ {
		hour := start.Add(time.Duration(i) * time.Hour).Format("2006-01-02T15:00:00Z")
		out = append(out, HourlyRequestStat{Hour: hour, Count: counts[hour]})
	}
	return out, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(sqliteTimeLayout)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(sqliteTimeLayout, value)
	if err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

type promptTemplateScanner interface {
	Scan(dest ...interface{}) error
}

func scanPromptTemplate(scanner promptTemplateScanner) (PromptTemplate, error) {
	var template PromptTemplate
	var created string
	var updated string
	if err := scanner.Scan(&template.ID, &template.Name, &template.SystemPrompt, &template.Prompt, &template.Model, &created, &updated); err != nil {
		return template, err
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return template, err
	}
	updatedAt, err := parseTime(updated)
	if err != nil {
		return template, err
	}
	template.CreatedAt = createdAt
	template.UpdatedAt = updatedAt
	return template, nil
}

// PromptTemplateByID returns a single prompt template.
func (s *Store) PromptTemplateByID(ctx context.Context, id string) (PromptTemplate, bool, error) {
	row := s.conn.QueryRowContext(ctx, `
		SELECT id, name, system_prompt, prompt, model, created_at, updated_at
		FROM prompt_templates
		WHERE id = ?
	`, id)
	template, err := scanPromptTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PromptTemplate{}, false, nil
	}
	if err != nil {
		return PromptTemplate{}, false, err
	}
	return template, true, nil
}

func cloneAPIKeySecrets(in []APIKeySecret) []APIKeySecret {
	out := make([]APIKeySecret, len(in))
	copy(out, in)
	return out
}

func (s *Store) invalidateActiveKeySecrets() {
	s.activeKeysMu.Lock()
	defer s.activeKeysMu.Unlock()
	s.activeKeys = nil
	s.activeKeysExpires = time.Time{}
}
