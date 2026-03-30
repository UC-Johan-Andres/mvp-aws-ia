package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"

	"launcher/config"
)

// N8NStatsSeriesItem: una fila por usuario (consulta PostgreSQL).
type N8NStatsSeriesItem struct {
	UserID                string  `json:"userId"`
	Email                 string  `json:"email"`
	WorkflowsAccesibles   int64   `json:"workflowsAccesibles"`
	TotalExecutions       int64   `json:"totalExecutions"`
	ProdExecutions        int64   `json:"prodExecutions"`
	FailedTotalExecutions int64   `json:"failedTotalExecutions"`
	FailedProdExecutions  int64   `json:"failedProdExecutions"`
	FailureRatePct        float64 `json:"failureRatePct"`
	RunTimeAvgSeconds     float64 `json:"runTimeAvgSeconds"`
}

// Incluye u.id para cruzar con la API aunque el email difiera en mayúsculas/espacios.
// ORDER BY usa la expresión (no alias) por compatibilidad entre versiones de PostgreSQL.
const n8nUserStatsSQL = `
SELECT 
  u.id::text AS user_id,
  u.email AS usuario,
  COUNT(DISTINCT sw."workflowId")::bigint AS workflows_accesibles,
  COUNT(e.id)::bigint AS total_executions,
  COUNT(e.id) FILTER (WHERE e.mode <> 'manual')::bigint AS prod_executions,
  COUNT(e.id) FILTER (WHERE e.status = 'error')::bigint AS failed_total_executions,
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
ORDER BY COUNT(e.id) DESC
`

type n8nDBStatsRow struct {
	WorkflowsAccesibles   int64
	TotalExecutions       int64
	ProdExecutions        int64
	FailedTotalExecutions int64
	FailedProdExecutions  int64
	FailureRatePct        float64
	RunTimeAvgSeconds     float64
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
			&r.TotalExecutions,
			&r.ProdExecutions,
			&r.FailedTotalExecutions,
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

func loadN8NStatsSeries(ctx context.Context, db *sql.DB) ([]N8NStatsSeriesItem, error) {
	rows, err := db.QueryContext(ctx, n8nUserStatsSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []N8NStatsSeriesItem
	for rows.Next() {
		var it N8NStatsSeriesItem
		if err := rows.Scan(
			&it.UserID,
			&it.Email,
			&it.WorkflowsAccesibles,
			&it.TotalExecutions,
			&it.ProdExecutions,
			&it.FailedTotalExecutions,
			&it.FailedProdExecutions,
			&it.FailureRatePct,
			&it.RunTimeAvgSeconds,
		); err != nil {
			return nil, err
		}
		it.UserID = strings.TrimSpace(it.UserID)
		it.Email = strings.TrimSpace(it.Email)
		out = append(out, it)
	}
	return out, rows.Err()
}

// FetchN8NStatsSeries ejecuta la misma consulta que enriquece usuarios y devuelve filas ordenadas para APIs/gráficos.
func FetchN8NStatsSeries(ctx context.Context) ([]N8NStatsSeriesItem, error) {
	dsn := config.N8NPostgresDSN()
	if dsn == "" {
		return nil, fmt.Errorf("postgres DSN no configurado (N8N_POSTGRES_DSN o DB_POSTGRESDB_* en .env.n8n)")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return loadN8NStatsSeries(ctx, db)
}

var warnedN8NNoDSN sync.Once

// n8nPersonalProjectsSQL: la API interna /rest/users a veces no incluye projectRelations;
// leemos el proyecto personal (RBAC) desde la misma BD que usa n8n.
const n8nPersonalProjectsSQL = `
SELECT u.id::text, lower(trim(both from u.email)), pr."projectId"::text, pr.role, p.name
FROM project_relation pr
JOIN "user" u ON u.id = pr."userId"
JOIN project p ON p.id = pr."projectId"
WHERE pr.role = 'project:personalOwner'
`

func loadN8NPersonalProjectMaps(ctx context.Context, db *sql.DB) (byUserID, byEmail map[string]N8NProjectRelation, err error) {
	rows, err := db.QueryContext(ctx, n8nPersonalProjectsSQL)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	byUserID = make(map[string]N8NProjectRelation)
	byEmail = make(map[string]N8NProjectRelation)
	for rows.Next() {
		var uid, em, pid, role string
		var pname sql.NullString
		if err := rows.Scan(&uid, &em, &pid, &role, &pname); err != nil {
			return nil, nil, err
		}
		rel := N8NProjectRelation{
			ID:   strings.TrimSpace(pid),
			Role: strings.TrimSpace(role),
			Name: strings.TrimSpace(pname.String),
		}
		uid = strings.TrimSpace(uid)
		em = strings.TrimSpace(em)
		if uid != "" {
			byUserID[strings.ToLower(uid)] = rel
		}
		if em != "" {
			byEmail[em] = rel
		}
	}
	return byUserID, byEmail, rows.Err()
}

// applyPersonalProjectsFromMaps rellena ProjectRelations cuando la API /rest/users viene vacía.
func applyPersonalProjectsFromMaps(users []N8NUser, byUserID, byEmail map[string]N8NProjectRelation) int {
	n := 0
	for i := range users {
		if len(users[i].ProjectRelations) > 0 {
			continue
		}
		uid := strings.TrimSpace(strings.ToLower(users[i].ID))
		key := strings.TrimSpace(strings.ToLower(users[i].Email))
		var rel N8NProjectRelation
		var ok bool
		if uid != "" {
			rel, ok = byUserID[uid]
		}
		if !ok && key != "" {
			rel, ok = byEmail[key]
		}
		if !ok {
			continue
		}
		users[i].ProjectRelations = []N8NProjectRelation{rel}
		n++
	}
	return n
}

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

	pByUser, pByEmail, err := loadN8NPersonalProjectMaps(ctx, db)
	if err != nil {
		log.Printf("n8n postgres: projectRelations SQL: %v", err)
	} else {
		log.Printf("n8n postgres: %d proyecto(s) personal(es) en BD (mapa por usuario)", len(pByUser))
		if n := applyPersonalProjectsFromMaps(users, pByUser, pByEmail); n > 0 {
			log.Printf("n8n postgres: projectRelations aplicados a %d usuario(s) (API sin projectRelations)", n)
		} else if len(users) > 0 && len(pByUser) > 0 {
			log.Printf("n8n postgres: aviso: hay %d proyectos personales en BD pero 0 coincidencias con %d usuario(s) de la API (revisar id/email)", len(pByUser), len(users))
		}
	}

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
		users[i].TotalExecutions = s.TotalExecutions
		users[i].ProdExecutions = s.ProdExecutions
		users[i].FailedTotalExecutions = s.FailedTotalExecutions
		users[i].FailedProdExecutions = s.FailedProdExecutions
		users[i].FailureRatePct = s.FailureRatePct
		users[i].RunTimeAvgSeconds = s.RunTimeAvgSeconds
	}
}
