package runcatalog

import (
	"context"
	"fmt"
	"strings"
)

type Run struct {
	RunID, RequestedBy, StartedAt, CompletedAt, TerminationReason string
	EventCount, RawBatchCount                                     uint64
	CaptureRaw, JournalTransport, Incomplete, Available           bool
	ManifestPath, ManifestSHA256, ConfigurationSHA256             string
}

type Query struct {
	StartedAfter, StartedBefore, TerminationReason string
	MinimumEventCount                              uint64
	Configuration                                  []Predicate
	Limit                                          int
	IncludeUnavailable                             bool
}

type Predicate struct {
	Layer, Parameter string
	Board, Channel   *int
	IntegerEqual     *int64
	IntegerMinimum   *int64
	IntegerMaximum   *int64
	RealEqual        *float64
	RealMinimum      *float64
	RealMaximum      *float64
	TextEqual        *string
}

// List returns newest runs first. Configuration predicates are combined with
// AND and each may independently constrain layer and scope.
func (c *Catalog) List(ctx context.Context, query Query) ([]Run, error) {
	if query.Limit < 0 || query.Limit > 1000 {
		return nil, fmt.Errorf("limit must be between 0 and 1000")
	}
	where, args := []string{"1=1"}, []any{}
	if !query.IncludeUnavailable {
		where = append(where, "r.available = 1")
	}
	if query.StartedAfter != "" {
		where = append(where, "r.started_at >= ?")
		args = append(args, query.StartedAfter)
	}
	if query.StartedBefore != "" {
		where = append(where, "r.started_at <= ?")
		args = append(args, query.StartedBefore)
	}
	if query.TerminationReason != "" {
		where = append(where, "r.termination_reason = ?")
		args = append(args, query.TerminationReason)
	}
	if query.MinimumEventCount > 0 {
		where = append(where, "r.event_count >= ?")
		args = append(args, query.MinimumEventCount)
	}
	for i, predicate := range query.Configuration {
		clause, values, err := predicateSQL(i, predicate)
		if err != nil {
			return nil, err
		}
		where = append(where, clause)
		args = append(args, values...)
	}
	statement := `SELECT r.run_id, r.requested_by, r.started_at, COALESCE(r.completed_at,''),
COALESCE(r.termination_reason,''), r.event_count, r.raw_batch_count, r.capture_raw,
r.journal_transport, r.incomplete, r.available, r.manifest_path, r.manifest_sha256,
COALESCE(r.configuration_sha256,'') FROM runs r WHERE ` + strings.Join(where, " AND ") + ` ORDER BY r.started_at DESC, r.run_id DESC`
	if query.Limit > 0 {
		statement += " LIMIT ?"
		args = append(args, query.Limit)
	}
	rows, err := c.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query run catalog: %w", err)
	}
	defer rows.Close()
	var result []Run
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.RunID, &run.RequestedBy, &run.StartedAt, &run.CompletedAt,
			&run.TerminationReason, &run.EventCount, &run.RawBatchCount, &run.CaptureRaw,
			&run.JournalTransport, &run.Incomplete, &run.Available, &run.ManifestPath, &run.ManifestSHA256,
			&run.ConfigurationSHA256); err != nil {
			return nil, fmt.Errorf("scan run catalog: %w", err)
		}
		result = append(result, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run catalog: %w", err)
	}
	return result, nil
}

func predicateSQL(index int, p Predicate) (string, []any, error) {
	if p.Parameter == "" {
		return "", nil, fmt.Errorf("configuration predicate %d requires a parameter", index)
	}
	conditions, args := []string{"c.run_id = r.run_id", "c.parameter = ?"}, []any{p.Parameter}
	if p.Layer != "" {
		conditions = append(conditions, "c.layer = ?")
		args = append(args, p.Layer)
	}
	if p.Board != nil {
		conditions = append(conditions, "c.board_index = ?")
		args = append(args, *p.Board)
	}
	if p.Channel != nil {
		conditions = append(conditions, "c.channel_index = ?")
		args = append(args, *p.Channel)
	}
	comparisons := 0
	add := func(value any, expression string) {
		comparisons++
		conditions = append(conditions, expression)
		args = append(args, value)
	}
	if p.IntegerEqual != nil {
		add(*p.IntegerEqual, "c.integer_value = ?")
	}
	if p.IntegerMinimum != nil {
		add(*p.IntegerMinimum, "c.integer_value >= ?")
	}
	if p.IntegerMaximum != nil {
		add(*p.IntegerMaximum, "c.integer_value <= ?")
	}
	if p.RealEqual != nil {
		add(*p.RealEqual, "c.real_value = ?")
	}
	if p.RealMinimum != nil {
		add(*p.RealMinimum, "c.real_value >= ?")
	}
	if p.RealMaximum != nil {
		add(*p.RealMaximum, "c.real_value <= ?")
	}
	if p.TextEqual != nil {
		add(*p.TextEqual, "c.text_value = ?")
	}
	if comparisons == 0 {
		return "", nil, fmt.Errorf("configuration predicate %d requires a comparison", index)
	}
	return "EXISTS (SELECT 1 FROM configuration_values c WHERE " + strings.Join(conditions, " AND ") + ")", args, nil
}
