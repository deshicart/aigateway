// Package analytics records lightweight usage stats and serves summaries.
package analytics

import (
	"database/sql"
	"time"
)

type Summary struct {
	Total        int            `json:"total"`
	Success      int            `json:"success"`
	Failed       int            `json:"failed"`
	SuccessRate  float64        `json:"success_rate"`
	AvgLatencyMs float64        `json:"avg_latency_ms"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
	Fallbacks    int            `json:"fallbacks"`
	ByProvider   map[string]int `json:"by_provider"`
	ByModel      map[string]int `json:"by_model"`
}

func Record(db *sql.DB, provider, model, keyID string, success bool, latencyMs int, in, out, fallbacks int, errMsg string) {
	s := 0
	if success {
		s = 1
	}
	if errMsg != "" && len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	_, _ = db.Exec(`INSERT INTO usage_stats(ts,provider,model,gateway_key_id,success,latency_ms,input_tokens,output_tokens,fallbacks,error) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		time.Now().UTC().Format(time.RFC3339Nano), provider, model, keyID, s, latencyMs, in, out, fallbacks, errMsg)
	// light retention: keep max 50k rows (100k in normal, 10k low-resource handled by caller)
	_, _ = db.Exec(`DELETE FROM usage_stats WHERE id NOT IN (SELECT id FROM usage_stats ORDER BY id DESC LIMIT 50000)`)
}

func Summarize(db *sql.DB, sinceHours int) Summary {
	s := Summary{ByProvider: map[string]int{}, ByModel: map[string]int{}}
	since := time.Now().UTC().Add(-time.Duration(sinceHours) * time.Hour).Format(time.RFC3339Nano)
	row := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(success),0), COALESCE(AVG(latency_ms),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(fallbacks),0) FROM usage_stats WHERE ts>=?`, since)
	var total, succ int
	var avg float64
	var in, out, fb sql.NullInt64
	_ = row.Scan(&total, &succ, &avg, &in, &out, &fb)
	s.Total, s.Success, s.AvgLatencyMs = total, succ, avg
	s.Failed = total - succ
	if total > 0 {
		s.SuccessRate = float64(succ) * 100 / float64(total)
	}
	if in.Valid {
		s.InputTokens = int(in.Int64)
	}
	if out.Valid {
		s.OutputTokens = int(out.Int64)
	}
	if fb.Valid {
		s.Fallbacks = int(fb.Int64)
	}
	rows, err := db.Query(`SELECT provider,COUNT(*) FROM usage_stats WHERE ts>=? GROUP BY provider`, since)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			var c int
			if err := rows.Scan(&p, &c); err == nil {
				s.ByProvider[p] = c
			}
		}
	}
	rows2, err := db.Query(`SELECT model,COUNT(*) FROM usage_stats WHERE ts>=? GROUP BY model ORDER BY COUNT(*) DESC LIMIT 20`, since)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var m string
			var c int
			if err := rows2.Scan(&m, &c); err == nil {
				s.ByModel[m] = c
			}
		}
	}
	return s
}
