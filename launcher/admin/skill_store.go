package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"launcher/config"
)

type SkillType string

const (
	SkillTypeAgent    SkillType = "agent"
	SkillTypeMCP      SkillType = "mcp"
	SkillTypeExternal SkillType = "external"
)

type Skill struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Type           SkillType `json:"type"`
	SourceID       string    `json:"sourceId,omitempty"`
	WorkflowName   string    `json:"workflowName,omitempty"`
	McpServerNames []string  `json:"mcpServerNames,omitempty"`
	Author         string    `json:"author,omitempty"`
	IsPublic       bool      `json:"isPublic"`
	AvailableInMCP bool      `json:"availableInMCP,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type skillStoreFile struct {
	Skills              []Skill             `json:"skills"`
	CompanyGroups       map[string]string   `json:"companyGroups"`
	CompanySkills       map[string][]string `json:"companySkills"`
	DefaultSkills       []string            `json:"defaultSkills"`
	DefaultCompany      string              `json:"defaultCompany"`
	MigrationsCompleted bool                `json:"migrationsCompleted"`
	LastMigrationAt     *time.Time          `json:"lastMigrationAt,omitempty"`
}

var (
	skillStoreMu     sync.RWMutex
	skillStoreLoaded bool
	skillStoreData   skillStoreFile
)

func ensureSkillMaps() {
	if skillStoreData.CompanyGroups == nil {
		skillStoreData.CompanyGroups = make(map[string]string)
	}
	if skillStoreData.CompanySkills == nil {
		skillStoreData.CompanySkills = make(map[string][]string)
	}
	if skillStoreData.DefaultSkills == nil {
		skillStoreData.DefaultSkills = []string{}
	}
}

func normalizeSkillStoreLocked() {
	if skillStoreData.DefaultCompany == "" {
		skillStoreData.DefaultCompany = "default"
	}
}

func loadSkillStoreFromDisk() error {
	path := config.GestionSkillStorePath()
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	ensureSkillMaps()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			skillStoreData.Skills = []Skill{}
			skillStoreData.CompanyGroups = make(map[string]string)
			skillStoreData.CompanySkills = make(map[string][]string)
			skillStoreData.DefaultSkills = []string{}
			skillStoreData.DefaultCompany = "default"
			skillStoreData.MigrationsCompleted = false
			skillStoreLoaded = true
			return saveSkillStoreLocked()
		}
		skillStoreLoaded = true
		return err
	}
	var f skillStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		skillStoreLoaded = true
		return err
	}
	skillStoreData = f
	ensureSkillMaps()
	normalizeSkillStoreLocked()
	skillStoreLoaded = true
	return saveSkillStoreLocked()
}

func saveSkillStoreLocked() error {
	path := config.GestionSkillStorePath()
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	ensureSkillMaps()
	normalizeSkillStoreLocked()
	b, err := json.MarshalIndent(skillStoreData, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func InitSkillStore() error {
	return loadSkillStoreFromDisk()
}

func GetSkillStorePath() string {
	return config.GestionSkillStorePath()
}

func AddSkill(skill Skill) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	for i, s := range skillStoreData.Skills {
		if s.ID == skill.ID {
			skillStoreData.Skills[i] = skill
			return saveSkillStoreLocked()
		}
	}

	skillStoreData.Skills = append(skillStoreData.Skills, skill)
	return saveSkillStoreLocked()
}

func RemoveSkill(skillID string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	for i, s := range skillStoreData.Skills {
		if s.ID == skillID {
			skillStoreData.Skills = append(skillStoreData.Skills[:i], skillStoreData.Skills[i+1:]...)
			break
		}
	}

	for company, skills := range skillStoreData.CompanySkills {
		newSkills := []string{}
		for _, sid := range skills {
			if sid != skillID {
				newSkills = append(newSkills, sid)
			}
		}
		skillStoreData.CompanySkills[company] = newSkills
	}

	for i, sid := range skillStoreData.DefaultSkills {
		if sid == skillID {
			skillStoreData.DefaultSkills = append(skillStoreData.DefaultSkills[:i], skillStoreData.DefaultSkills[i+1:]...)
			break
		}
	}

	return saveSkillStoreLocked()
}

func GetSkillByID(skillID string) (Skill, bool) {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	for _, s := range skillStoreData.Skills {
		if s.ID == skillID {
			return s, true
		}
	}
	return Skill{}, false
}

func GetAllSkills() []Skill {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	out := make([]Skill, len(skillStoreData.Skills))
	copy(out, skillStoreData.Skills)
	return out
}

func GetAgentSkills() []Skill {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	var out []Skill
	for _, s := range skillStoreData.Skills {
		if s.Type == SkillTypeAgent {
			out = append(out, s)
		}
	}
	return out
}

func GetMCPSkills() []Skill {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	var out []Skill
	for _, s := range skillStoreData.Skills {
		if s.Type == SkillTypeMCP {
			out = append(out, s)
		}
	}
	return out
}

func AddSkillToCompany(skillID, company string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	company = strings.TrimSpace(company)
	if company == "" {
		company = "default"
	}

	if _, ok := skillStoreData.CompanySkills[company]; !ok {
		skillStoreData.CompanySkills[company] = []string{}
	}

	for _, sid := range skillStoreData.CompanySkills[company] {
		if sid == skillID {
			return nil
		}
	}

	skillStoreData.CompanySkills[company] = append(skillStoreData.CompanySkills[company], skillID)
	return saveSkillStoreLocked()
}

func RemoveSkillFromCompany(skillID, company string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	skills, ok := skillStoreData.CompanySkills[company]
	if !ok {
		return nil
	}

	newSkills := []string{}
	for _, sid := range skills {
		if sid != skillID {
			newSkills = append(newSkills, sid)
		}
	}
	skillStoreData.CompanySkills[company] = newSkills

	return saveSkillStoreLocked()
}

func GetSkillsForCompany(company string) []Skill {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	skillIDs, ok := skillStoreData.CompanySkills[company]
	if !ok {
		return []Skill{}
	}

	var out []Skill
	for _, sid := range skillIDs {
		for _, s := range skillStoreData.Skills {
			if s.ID == sid {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func GetCompanyGroupID(company string) (string, bool) {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	groupID, ok := skillStoreData.CompanyGroups[company]
	return groupID, ok
}

func SaveCompanyGroupID(company, groupID string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	skillStoreData.CompanyGroups[company] = groupID
	return saveSkillStoreLocked()
}

func AddDefaultSkill(skillID string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	for _, sid := range skillStoreData.DefaultSkills {
		if sid == skillID {
			return nil
		}
	}

	skillStoreData.DefaultSkills = append(skillStoreData.DefaultSkills, skillID)
	return saveSkillStoreLocked()
}

func RemoveDefaultSkill(skillID string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	newDefaults := []string{}
	for _, sid := range skillStoreData.DefaultSkills {
		if sid != skillID {
			newDefaults = append(newDefaults, sid)
		}
	}
	skillStoreData.DefaultSkills = newDefaults

	return saveSkillStoreLocked()
}

func GetDefaultSkills() []string {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	out := make([]string, len(skillStoreData.DefaultSkills))
	copy(out, skillStoreData.DefaultSkills)
	return out
}

func IsMigrationCompleted() bool {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()
	return skillStoreData.MigrationsCompleted
}

func MarkMigrationCompleted() error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	skillStoreData.MigrationsCompleted = true
	now := time.Now()
	skillStoreData.LastMigrationAt = &now
	return saveSkillStoreLocked()
}

func GetAllCompaniesInSkillStore() []string {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	companies := make(map[string]bool)
	for company := range skillStoreData.CompanySkills {
		companies[company] = true
	}
	for company := range skillStoreData.CompanyGroups {
		companies[company] = true
	}

	out := make([]string, 0, len(companies))
	for c := range companies {
		out = append(out, c)
	}
	return out
}

func RemoveCompanyFromSkillStore(company string) error {
	skillStoreMu.Lock()
	defer skillStoreMu.Unlock()

	if !skillStoreLoaded {
		return fmt.Errorf("skill store not initialized")
	}

	delete(skillStoreData.CompanySkills, company)
	delete(skillStoreData.CompanyGroups, company)

	return saveSkillStoreLocked()
}

func GetCompanySkillIDs(company string) ([]string, bool) {
	skillStoreMu.RLock()
	defer skillStoreMu.RUnlock()

	skills, ok := skillStoreData.CompanySkills[company]
	if !ok {
		return []string{}, false
	}
	out := make([]string, len(skills))
	copy(out, skills)
	return out, true
}

func MarkSkillSynced(skillID, company string) error {
	return nil
}
