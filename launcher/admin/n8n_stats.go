package admin

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"launcher/config"
)

// Incluye u.id para cruzar con la API aunque el email difiera en mayúsculas/espacios.
// ORDER BY usa la expresión (no alias) por compatibilidad entre versiones de PostgreSQL.
const n8nUserStatsSQL = `
SELECT 
  u.id::text AS user_id,
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
GROUP BY u.id, u.email
ORDER BY COUNT(e.id) FILTER (WHERE e.mode <> 'manual') DESC
`

type n8nDBStatsRow struct {
	WorkflowsAccesibles  int64
	ProdExecutions       int64
	FailedProdExecutions int64
	FailureRatePct       float64
	RunTimeAvgSeconds    float64
}

func loadN8NStatsMaps(ctx context.Context, db *sql.DB) (byUserID map[string]n8nDBStatsRow, byEmail map[string]n8nDBStatsRow, err error) {
	rows, err := db.QueryContext(ctx, n8nUserStatsSQL)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	byUserID = make(map[string]n8nDBStatsRow)
	byEmail = make(map[string]n8nDBStatsRow)
	for rows.Next() {
		var userID, email string
		var r n8nDBStatsRow
		if err := rows.Scan(
			&userID,
			&email,
			&r.WorkflowsAccesibles,
			&r.ProdExecutions,
			&r.FailedProdExecutions,
			&r.FailureRatePct,
			&r.RunTimeAvgSeconds,
		); err != nil {
			return nil, nil, err
		}
		userID = strings.TrimSpace(userID)
		em := strings.TrimSpace(strings.ToLower(email))
		byUserID[userID] = r
		byEmail[em] = r
	}
	return byUserID, byEmail, rows.Err()
}

var warnedN8NNoDSN sync.Once

func enrichN8NUsersFromPostgres(users []N8NUser) {
	dsn := config.N8NPostgresDSN()
	if dsn == "" {
		warnedN8NNoDSN.Do(func() {
			log.Print("n8n postgres: sin DSN: defina DB_POSTGRESDB_PASSWORD en .env.n8n (o N8N_POSTGRES_DSN). Las métricas quedan en 0.")
		})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Printf("n8n postgres: open: %v", err)
		return
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		log.Printf("n8n postgres: ping: %v (compruebe DSN, red Docker y credenciales de la BD n8n)", err)
		return
	}

	byID, byEmail, err := loadN8NStatsMaps(ctx, db)
	if err != nil {
		log.Printf("n8n postgres: consulta SQL: %v", err)
		return
	}

	log.Printf("n8n postgres: %d filas de estadísticas en BD (fusionando por id/email)", len(byID))

	for i := range users {
		var s n8nDBStatsRow
		var ok bool
		if uid := strings.TrimSpace(users[i].ID); uid != "" {
			s, ok = byID[uid]
		}
		if !ok {
			key := strings.TrimSpace(strings.ToLower(users[i].Email))
			s, ok = byEmail[key]
		}
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
