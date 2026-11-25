package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func newAggregationID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("agg-%d", time.Now().UnixNano())
	}
	return "agg-" + hex.EncodeToString(raw)
}

func (s *Store) InsertAggregation(ctx context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	if strings.TrimSpace(agg.ID) == "" {
		agg.ID = newAggregationID()
	}
	if agg.CreatedAt.IsZero() {
		agg.CreatedAt = time.Now().UTC()
	}
	if agg.UpdatedAt.IsZero() {
		agg.UpdatedAt = agg.CreatedAt
	}
	if agg.Members == nil {
		agg.Members = []accounts.AggregationMember{}
	}
	changed, err := s.saveAggregation(ctx, agg, true)
	if err != nil {
		return accounts.Aggregation{}, err
	}
	if !changed {
		return accounts.Aggregation{}, accounts.ErrAggregationNotFound
	}
	return s.GetAggregation(ctx, agg.ID)
}

func (s *Store) SaveAggregation(ctx context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	if agg.UpdatedAt.IsZero() {
		agg.UpdatedAt = time.Now().UTC()
	}
	if agg.Members == nil {
		agg.Members = []accounts.AggregationMember{}
	}
	changed, err := s.saveAggregation(ctx, agg, false)
	if err != nil {
		return accounts.Aggregation{}, err
	}
	if !changed {
		return accounts.Aggregation{}, accounts.ErrAggregationNotFound
	}
	return s.GetAggregation(ctx, agg.ID)
}

func (s *Store) ListAggregations(ctx context.Context) ([]accounts.Aggregation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, enabled, strategy, allow_fallbacks, max_attempts, context_length, max_output_tokens, reasoning_options, reasoning_effort, created_at, updated_at
FROM aggregation_models ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list aggregations: %w", err)
	}
	defer rows.Close()
	var list []accounts.Aggregation
	for rows.Next() {
		agg, err := scanAggregation(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, agg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range list {
		members, err := s.listAggregationMembers(ctx, list[i].ID)
		if err != nil {
			return nil, err
		}
		list[i].Members = members
	}
	return list, nil
}

func (s *Store) GetAggregation(ctx context.Context, id string) (accounts.Aggregation, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, enabled, strategy, allow_fallbacks, max_attempts, context_length, max_output_tokens, reasoning_options, reasoning_effort, created_at, updated_at
FROM aggregation_models WHERE id = ?`, strings.TrimSpace(id))
	agg, err := scanAggregation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.Aggregation{}, accounts.ErrAggregationNotFound
	}
	if err != nil {
		return accounts.Aggregation{}, fmt.Errorf("get aggregation: %w", err)
	}
	members, err := s.listAggregationMembers(ctx, agg.ID)
	if err != nil {
		return accounts.Aggregation{}, err
	}
	agg.Members = members
	return agg, nil
}

func (s *Store) DeleteAggregation(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM aggregation_models WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete aggregation: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return accounts.ErrAggregationNotFound
	}
	return nil
}

// saveAggregation writes the body and its members in one transaction so a
// reader never observes a body without members (or a stale member set).
// It returns false when an update targeted a missing body.
func (s *Store) saveAggregation(ctx context.Context, agg accounts.Aggregation, insert bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var result sql.Result
	if insert {
		result, err = tx.ExecContext(ctx, `
INSERT INTO aggregation_models (id, name, enabled, strategy, allow_fallbacks, max_attempts, context_length, max_output_tokens, reasoning_options, reasoning_effort, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			agg.ID, agg.Name, boolToInt(agg.Enabled), string(agg.Strategy), boolToInt(agg.AllowFallbacks), agg.MaxAttempts,
			agg.ContextLength, agg.MaxOutputTokens, encodeAggregationReasoningOptions(agg.ReasoningOptions), agg.ReasoningEffort,
			formatTime(agg.CreatedAt), formatTime(agg.UpdatedAt),
		)
	} else {
		result, err = tx.ExecContext(ctx, `
UPDATE aggregation_models SET name = ?, enabled = ?, strategy = ?, allow_fallbacks = ?, max_attempts = ?, context_length = ?, max_output_tokens = ?, reasoning_options = ?, reasoning_effort = ?, updated_at = ?
WHERE id = ?`,
			agg.Name, boolToInt(agg.Enabled), string(agg.Strategy), boolToInt(agg.AllowFallbacks), agg.MaxAttempts,
			agg.ContextLength, agg.MaxOutputTokens, encodeAggregationReasoningOptions(agg.ReasoningOptions), agg.ReasoningEffort,
			formatTime(agg.UpdatedAt), agg.ID,
		)
	}
	if err != nil {
		return false, fmt.Errorf("save aggregation body: %w", err)
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		return false, nil
	}
	if err := s.saveAggregationMembersTx(ctx, tx, agg.ID, agg.Members); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) saveAggregationMembersTx(ctx context.Context, tx *sql.Tx, id string, members []accounts.AggregationMember) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM aggregation_members WHERE agg_id = ?`, id); err != nil {
		return fmt.Errorf("clear aggregation members: %w", err)
	}
	for _, member := range members {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO aggregation_members (agg_id, provider, region, model, account_id, priority, weight)
VALUES (?, ?, ?, ?, ?, ?, ?)`, id, member.Provider, member.Region, member.Model, member.AccountID, member.Priority, member.Weight); err != nil {
			return fmt.Errorf("save aggregation member: %w", err)
		}
	}
	return nil
}

func (s *Store) listAggregationMembers(ctx context.Context, id string) ([]accounts.AggregationMember, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, provider, region, model, account_id, priority, weight
FROM aggregation_members WHERE agg_id = ? ORDER BY priority ASC, id ASC`, strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("list aggregation members: %w", err)
	}
	defer rows.Close()
	members := []accounts.AggregationMember{}
	for rows.Next() {
		var member accounts.AggregationMember
		if err := rows.Scan(&member.ID, &member.Provider, &member.Region, &member.Model, &member.AccountID, &member.Priority, &member.Weight); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func scanAggregation(row rowScanner) (accounts.Aggregation, error) {
	var agg accounts.Aggregation
	var enabled, allowFallbacks int
	var reasoningOptions, reasoningEffort string
	var created, updated sql.NullString
	if err := row.Scan(&agg.ID, &agg.Name, &enabled, &agg.Strategy, &allowFallbacks, &agg.MaxAttempts,
		&agg.ContextLength, &agg.MaxOutputTokens, &reasoningOptions, &reasoningEffort, &created, &updated); err != nil {
		return accounts.Aggregation{}, err
	}
	agg.Enabled = enabled != 0
	agg.Strategy = accounts.NormalizeAggregationStrategy(string(agg.Strategy))
	agg.AllowFallbacks = allowFallbacks != 0
	agg.ReasoningOptions = decodeAggregationReasoningOptions(reasoningOptions)
	agg.ReasoningEffort = strings.TrimSpace(reasoningEffort)
	agg.CreatedAt = parseTime(created.String)
	agg.UpdatedAt = parseTime(updated.String)
	agg.Members = []accounts.AggregationMember{}
	return agg, nil
}

func encodeAggregationReasoningOptions(options []string) string {
	if len(options) == 0 {
		return ""
	}
	payload, err := json.Marshal(options)
	if err != nil {
		return ""
	}
	return string(payload)
}

func decodeAggregationReasoningOptions(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var options []string
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return nil
	}
	return options
}
