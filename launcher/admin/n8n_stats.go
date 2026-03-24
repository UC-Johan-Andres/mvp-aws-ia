package admin

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"launcher/config"
)

// n8nStatsHTTPClient uses a longer timeout for aggregating workflow/execution stats.
func n8nStatsHTTPClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}

// n8nPersonalProjectID resolves the user's personal project UUID for workflow/execution scoping.
func n8nPersonalProjectID(relations []N8NProjectRelation) string {
	if len(relations) == 0 {
		return ""
	}
	for _, pr := range relations {
		if strings.Contains(strings.ToLower(pr.Role), "personalowner") {
			return pr.ID
		}
	}
	if len(relations) == 1 {
		return relations[0].ID
	}
	return ""
}

func n8nDoGET(client *http.Client, cookies []*http.Cookie, path string, q url.Values) ([]byte, int, error) {
	u := config.N8NInternalURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if config.N8NBasicUser != "" {
		req.SetBasicAuth(config.N8NBasicUser, config.N8NBasicPass)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

func n8nCountWorkflowsForProject(client *http.Client, cookies []*http.Cookie, projectID string) (total, active int, err error) {
	const pageSize = 250
	filterObj := map[string]string{"projectId": projectID}
	filterJSON, err := json.Marshal(filterObj)
	if err != nil {
		return 0, 0, err
	}

	skip := 0
	totalAll := -1
	for {
		q := url.Values{}
		q.Set("filter", string(filterJSON))
		q.Set("take", fmt.Sprintf("%d", pageSize))
		q.Set("skip", fmt.Sprintf("%d", skip))

		data, status, err := n8nDoGET(client, cookies, "/rest/workflows", q)
		if err != nil {
			return 0, 0, err
		}
		if status != http.StatusOK {
			return 0, 0, fmt.Errorf("workflows: status %d: %s", status, string(data))
		}

		var parsed struct {
			Count int `json:"count"`
			Data  []struct {
				Active          bool    `json:"active"`
				ActiveVersionID *string `json:"activeVersionId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return 0, 0, err
		}

		if totalAll < 0 {
			totalAll = parsed.Count
		}
		for _, w := range parsed.Data {
			if w.Active || (w.ActiveVersionID != nil && *w.ActiveVersionID != "") {
				active++
			}
		}

		if len(parsed.Data) == 0 || skip+len(parsed.Data) >= totalAll {
			break
		}
		skip += len(parsed.Data)
	}
	if totalAll < 0 {
		totalAll = 0
	}
	return totalAll, active, nil
}

func n8nCountExecutionsForProject(client *http.Client, cookies []*http.Cookie, projectID string) (int64, error) {
	tryFilters := []map[string]interface{}{
		{
			"projectId": projectID,
			"status": []string{
				"new", "running", "success", "error", "crashed", "canceled", "waiting",
			},
		},
		{"projectId": projectID},
	}

	for _, filterObj := range tryFilters {
		filterJSON, err := json.Marshal(filterObj)
		if err != nil {
			return 0, err
		}
		q := url.Values{}
		q.Set("filter", string(filterJSON))
		q.Set("take", "1")

		data, status, err := n8nDoGET(client, cookies, "/rest/executions", q)
		if err != nil {
			return 0, err
		}
		if status != http.StatusOK {
			if len(tryFilters) > 1 {
				continue
			}
			return 0, fmt.Errorf("executions: status %d: %s", status, string(data))
		}

		var parsed struct {
			Count int64 `json:"count"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return 0, err
		}
		return parsed.Count, nil
	}
	return 0, fmt.Errorf("executions: could not count for project %s", projectID)
}

func enrichN8NUsersWithStats(client *http.Client, cookies []*http.Cookie, users []N8NUser) {
	const workers = 6
	jobs := make(chan int, len(users))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				pid := n8nPersonalProjectID(users[i].ProjectRelations)
				if pid == "" {
					continue
				}
				wt, wa, err := n8nCountWorkflowsForProject(client, cookies, pid)
				if err != nil {
					log.Printf("n8n stats: workflows for user %s project %s: %v", users[i].Email, pid, err)
				} else {
					users[i].WorkflowsTotal = wt
					users[i].WorkflowsActive = wa
				}
				ec, err := n8nCountExecutionsForProject(client, cookies, pid)
				if err != nil {
					log.Printf("n8n stats: executions for user %s project %s: %v", users[i].Email, pid, err)
				} else {
					users[i].ExecutionsTotal = ec
				}
			}
		}()
	}

	for i := range users {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}
