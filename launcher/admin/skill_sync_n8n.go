package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"launcher/config"

	_ "github.com/lib/pq"
)

func getN8NPostgresDB() (*sql.DB, error) {
	dsn := config.N8NPostgresDSN()
	if dsn == "" {
		return nil, fmt.Errorf("N8NPostgresDSN vacía")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open n8n postgres: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

func ActivateMCPWorkflow(ctx context.Context, workflowName string) error {
	db, err := getN8NPostgresDB()
	if err != nil {
		return fmt.Errorf("ActivateMCPWorkflow: get db: %w", err)
	}
	defer db.Close()

	query := `UPDATE workflow_entity 
	SET settings = jsonb_set(settings, '{availableInMCP}', 'true') 
	WHERE name = $1`

	result, err := db.ExecContext(ctx, query, workflowName)
	if err != nil {
		return fmt.Errorf("ActivateMCPWorkflow: update: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("ActivateMCPWorkflow: %s → availableInMCP=true (rows affected: %d)", workflowName, rowsAffected)

	return nil
}

func DeactivateMCPWorkflow(ctx context.Context, workflowName string) error {
	db, err := getN8NPostgresDB()
	if err != nil {
		return fmt.Errorf("DeactivateMCPWorkflow: get db: %w", err)
	}
	defer db.Close()

	query := `UPDATE workflow_entity 
	SET settings = jsonb_set(settings, '{availableInMCP}', 'false') 
	WHERE name = $1`

	result, err := db.ExecContext(ctx, query, workflowName)
	if err != nil {
		return fmt.Errorf("DeactivateMCPWorkflow: update: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Printf("DeactivateMCPWorkflow: %s → availableInMCP=false (rows affected: %d)", workflowName, rowsAffected)

	return nil
}

type n8nWorkflow struct {
	ID        string
	Name      string
	Active    bool
	Settings  map[string]interface{}
	CreatedAt time.Time
}

func ListAvailableMCPWorkflows(ctx context.Context) ([]n8nWorkflow, error) {
	db, err := getN8NPostgresDB()
	if err != nil {
		return nil, fmt.Errorf("ListAvailableMCPWorkflows: get db: %w", err)
	}
	defer db.Close()

	query := `SELECT id, name, active, settings, createdAt 
	FROM workflow_entity 
	WHERE settings->>'availableInMCP' = 'true'
	ORDER BY name`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListAvailableMCPWorkflows: query: %w", err)
	}
	defer rows.Close()

	var workflows []n8nWorkflow
	for rows.Next() {
		var w n8nWorkflow
		var settingsJSON string
		err := rows.Scan(&w.ID, &w.Name, &w.Active, &settingsJSON, &w.CreatedAt)
		if err != nil {
			log.Printf("ListAvailableMCPWorkflows: scan error: %v", err)
			continue
		}
		workflows = append(workflows, w)
	}

	return workflows, nil
}

func ListAllN8NWorkflows(ctx context.Context) ([]n8nWorkflow, error) {
	db, err := getN8NPostgresDB()
	if err != nil {
		return nil, fmt.Errorf("ListAllN8NWorkflows: get db: %w", err)
	}
	defer db.Close()

	query := `SELECT id, name, active, settings, createdAt 
	FROM workflow_entity 
	ORDER BY name`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListAllN8NWorkflows: query: %w", err)
	}
	defer rows.Close()

	var workflows []n8nWorkflow
	for rows.Next() {
		var w n8nWorkflow
		var settingsJSON string
		err := rows.Scan(&w.ID, &w.Name, &w.Active, &settingsJSON, &w.CreatedAt)
		if err != nil {
			log.Printf("ListAllN8NWorkflows: scan error: %v", err)
			continue
		}
		workflows = append(workflows, w)
	}

	return workflows, nil
}

func IsMCPEnabled(workflowName string) (bool, error) {
	db, err := getN8NPostgresDB()
	if err != nil {
		return false, fmt.Errorf("IsMCPEnabled: get db: %w", err)
	}
	defer db.Close()

	query := `SELECT settings->>'availableInMCP' = 'true' 
	FROM workflow_entity 
	WHERE name = $1`

	var enabled bool
	err = db.QueryRowContext(context.Background(), query, workflowName).Scan(&enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("IsMCPEnabled: query: %w", err)
	}

	return enabled, nil
}

func GetMCPWorkflowID(workflowName string) (string, error) {
	db, err := getN8NPostgresDB()
	if err != nil {
		return "", fmt.Errorf("GetMCPWorkflowID: get db: %w", err)
	}
	defer db.Close()

	query := `SELECT id FROM workflow_entity WHERE name = $1`

	var id string
	err = db.QueryRowContext(context.Background(), query, workflowName).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("workflow not found: %s", workflowName)
		}
		return "", fmt.Errorf("GetMCPWorkflowID: query: %w", err)
	}

	return id, nil
}
