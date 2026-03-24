package admin

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"launcher/config"
)

// Equivalente a la consulta validada en psql sobre la BD n8n (execution_entity, modo <> 'manual' = prod).
const n8nUserStatsSQL = `
SELECT 
  u.email AS usuario,
  COUNT(DISTINCT sw."workflowId")::bigint AS workflows_accesibles,
  COUNT(e.id) FILTER (WHERE e.mode <> 'manual')::bigint AS prod_executions,
  COUNT(e.id) FILTER (WHERE e.mode <> 'manual' AND e.status = 'error')::bigint AS failed_prod_executions,
  CASE 
    WHEN COUNT(e.id) FILTER (WHERE e.mode <> 'manual') = 0 THEN 0::double precision
    ELSE ROUND(
      (COUNT(e.id) FILTER (WHERE e.mode <> 'manual' AND e.status = 'error')::numeric * 100.0)
      / NULLIF(COUNT(e.id) FILTER (WHERE e.mode <> 'manual'), 0), 2
    )::double precision
  END AS failure_rate_pct,
  COALESCE(
    ROUND(AVG(EXTRACT(EPOCH FROM (e."stoppedAt" - e."startedAt")))::numeric, 2),
    0
  )::double precision AS run_time_avg_s
FROM "user" u
LEFT JOIN project_relation pr ON u.id = pr."userId"
LEFT JOIN shared_workflow sw ON pr."projectId" = sw."projectId"
LEFT JOIN execution_entity e ON sw."workflowId" = e."workflowId"
GROUP BY u.email
ORDER BY prod_executions DESC
`

type n8nDBStatsRow struct {
	WorkflowsAccesibles  int64
	ProdExecutions       int64
	FailedProdExecutions int64
	FailureRatePct       float64
	RunTimeAvgSeconds    float64
}

func loadN8NStatsByEmail(ctx context.Context, db *sql.DB) (map[string]n8nDBStatsRow, error) {
	rows, err := db.QueryContext(ctx, n8nUserStatsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]n8nDBStatsRow)
	for rows.Next() {
		var email string
		var r n8nDBStatsRow
		if err := rows.Scan(
			&email,
			&r.WorkflowsAccesibles,
			&r.ProdExecutions,
			&r.FailedProdExecutions,
			&r.FailureRatePct,
			&r.RunTimeAvgSeconds,
		); err != nil {
			return nil, err
		}
		out[strings.TrimSpace(strings.ToLower(email))] = r
	}
	return out, rows.Err()
}

func enrichN8NUsersFromPostgres(users []N8NUser) {
	if config.N8NPostgresDSN == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("postgres", config.N8NPostgresDSN)
	if err != nil {
		log.Printf("n8n postgres: open: %v", err)
		return
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		log.Printf("n8n postgres: ping: %v", err)
		return
	}

	statsByEmail, err := loadN8NStatsByEmail(ctx, db)
	if err != nil {
		log.Printf("n8n postgres: stats query: %v", err)
		return
	}

	for i := range users {
		key := strings.TrimSpace(strings.ToLower(users[i].Email))
		s, ok := statsByEmail[key]
		if !ok {
			continue
		}
		users[i].WorkflowsAccesibles = s.WorkflowsAccesibles
		users[i].ProdExecutions = s.ProdExecutions
		users[i].FailedProdExecutions = s.FailedProdExecutions
		users[i].FailureRatePct = s.FailureRatePct
		users[i].RunTimeAvgSeconds = s.RunTimeAvgSeconds
	}
}
