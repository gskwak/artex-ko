package db

import (
	"context"
	"strings"
)

// BlueTrafficFilter searches imported defensive telemetry. It never reads the
// red-team recording proxy's traffic store.
type BlueTrafficFilter struct {
	Query    string
	Source   string
	Protocol string
	Limit    int
	Offset   int
}

// A lone source IP is common in authentication events, so it is insufficient
// evidence that a record describes a network exchange. A known network source
// or type, a flow/protocol object, or both endpoints is required.
// File import preserves the entire input object in blue_events.raw, including
// an optional nested "raw" object. Merge those fields for identical behavior
// between file upload and direct API import.
const blueTrafficFrom = ` FROM blue_events CROSS JOIN LATERAL (
	SELECT CASE WHEN jsonb_typeof(raw->'raw') = 'object'
		THEN (raw - 'raw') || (raw->'raw') ELSE raw END AS traffic_raw
) traffic_fields`

const blueTrafficPredicate = `(
	jsonb_exists_any(traffic_raw, ARRAY['flow_id','http','dns'])
	OR (
		jsonb_exists_any(traffic_raw, ARRAY['src_ip','source_ip','id.orig_h'])
		AND jsonb_exists_any(traffic_raw, ARRAY['dest_ip','dst_ip','destination_ip','id.resp_h'])
	)
	OR (traffic_raw #>> '{source,ip}' IS NOT NULL AND traffic_raw #>> '{destination,ip}' IS NOT NULL)
	OR lower(source) ~ '(^|[^a-z0-9])(zeek|suricata|firewall|waf|proxy|dns|http|https|network|traffic|netflow|flow)([^a-z0-9]|$)'
	OR lower(event_type) ~ '(^|[^a-z0-9])(network|traffic|http|https|dns|tls|connection|conn|netflow|flow|firewall|waf|proxy)([^a-z0-9]|$)'
)`

const blueTrafficWhere = ` WHERE ` + blueTrafficPredicate + `
	AND ($1::text = '' OR
		strpos(lower(source), lower($1::text)) > 0 OR
		strpos(lower(asset), lower($1::text)) > 0 OR
		strpos(lower(message), lower($1::text)) > 0 OR
		strpos(lower(event_type), lower($1::text)) > 0 OR
		strpos(lower(raw::text), lower($1::text)) > 0)
	AND ($2::text = '' OR strpos(lower(source), lower($2::text)) > 0)
	AND ($3::text = '' OR lower($3::text) = ANY(ARRAY[
		lower(COALESCE(traffic_raw->>'proto','')),
		lower(COALESCE(traffic_raw->>'protocol','')),
		lower(COALESCE(traffic_raw->>'network.transport','')),
		lower(COALESCE(traffic_raw #>> '{network,transport}','')),
		lower(COALESCE(traffic_raw->>'transport','')),
		lower(COALESCE(traffic_raw->>'app_proto','')),
		lower(COALESCE(traffic_raw->>'service','')),
		lower(COALESCE(traffic_raw->>'network.protocol','')),
		lower(COALESCE(traffic_raw #>> '{network,protocol}',''))
	]))`

// ListBlueTraffic returns a server-filtered page and the count before paging.
// The same predicate is used for both queries so unrelated events do not shift
// page boundaries or inflate the displayed total.
func (d *DB) ListBlueTraffic(ctx context.Context, filter BlueTrafficFilter) ([]BlueEvent, int64, error) {
	query := strings.TrimSpace(filter.Query)
	source := strings.TrimSpace(filter.Source)
	protocol := strings.TrimSpace(filter.Protocol)
	args := []any{query, source, protocol}

	var total int64
	if err := d.QueryRowContext(ctx, `SELECT count(*)`+blueTrafficFrom+blueTrafficWhere, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.QueryContext(ctx, `SELECT `+blueEventColumns+blueTrafficFrom+blueTrafficWhere+
		` ORDER BY occurred_at DESC,id DESC LIMIT $4 OFFSET $5`,
		query, source, protocol, blueLimit(filter.Limit), blueOffset(filter.Offset))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []BlueEvent{}
	for rows.Next() {
		event, err := scanBlueEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
