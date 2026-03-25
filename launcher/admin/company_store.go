package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"launcher/config"
)

type companyStoreFile struct {
	N8NByEmail map[string]string `json:"n8nByEmail"`
	N8NByID    map[string]string `json:"n8nByID"`
}

var (
	companyStoreMu     sync.RWMutex
	companyStoreLoaded bool
	companyStoreData   companyStoreFile
)

func ensureCompanyMaps() {
	if companyStoreData.N8NByEmail == nil {
		companyStoreData.N8NByEmail = make(map[string]string)
	}
	if companyStoreData.N8NByID == nil {
		companyStoreData.N8NByID = make(map[string]string)
	}
}

func loadCompanyStoreFromDisk() error {
	path := config.GestionCompanyStorePath()
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()

	ensureCompanyMaps()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			companyStoreLoaded = true
			return nil
		}
		companyStoreLoaded = true
		return err
	}
	var f companyStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		companyStoreLoaded = true
		return err
	}
	companyStoreData = f
	ensureCompanyMaps()
	companyStoreLoaded = true
	return nil
}

func saveCompanyStoreLocked() error {
	path := config.GestionCompanyStorePath()
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	ensureCompanyMaps()
	b, err := json.MarshalIndent(companyStoreData, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// InitCompanyStore carga el JSON de empresas n8n (si existe).
func InitCompanyStore() error {
	return loadCompanyStoreFromDisk()
}

func normEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// N8NUserCompany devuelve la empresa asociada a un usuario n8n (archivo local).
func N8NUserCompany(userID, email string) string {
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded {
		return config.GestionDefaultCompany()
	}
	id := strings.TrimSpace(userID)
	if id != "" {
		if c, ok := companyStoreData.N8NByID[id]; ok && strings.TrimSpace(c) != "" {
			return c
		}
	}
	if k := normEmail(email); k != "" {
		if c, ok := companyStoreData.N8NByEmail[k]; ok && strings.TrimSpace(c) != "" {
			return c
		}
	}
	return config.GestionDefaultCompany()
}

// SetN8NUserCompany guarda empresa para n8n (por email y opcionalmente por id).
func SetN8NUserCompany(userID, email, company string) error {
	c := strings.TrimSpace(company)
	if !config.IsValidGestionCompany(c) {
		return fmt.Errorf("empresa no válida: %q", company)
	}
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	if k := normEmail(email); k != "" {
		companyStoreData.N8NByEmail[k] = c
	}
	if id := strings.TrimSpace(userID); id != "" {
		companyStoreData.N8NByID[id] = c
	}
	return saveCompanyStoreLocked()
}

// ReconcileN8NCompanyIDs copia asignaciones por email a id cuando ya existe el usuario en n8n.
func ReconcileN8NCompanyIDs(users []N8NUser) {
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	if !companyStoreLoaded {
		return
	}
	ensureCompanyMaps()
	changed := false
	for _, u := range users {
		id := strings.TrimSpace(u.ID)
		k := normEmail(u.Email)
		if id == "" || k == "" {
			continue
		}
		if _, hasID := companyStoreData.N8NByID[id]; hasID {
			continue
		}
		if c, ok := companyStoreData.N8NByEmail[k]; ok && strings.TrimSpace(c) != "" {
			companyStoreData.N8NByID[id] = c
			changed = true
		}
	}
	if changed {
		_ = saveCompanyStoreLocked()
	}
}

// N8NEmailCompanyRow asigna empresa a un email de invitación n8n.
type N8NEmailCompanyRow struct {
	Email   string
	Company string // vacío → GestionDefaultCompany
}

// PersistN8NEmailCompanies escribe en el store local la empresa por email (invitaciones).
func PersistN8NEmailCompanies(rows []N8NEmailCompanyRow) error {
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	for _, row := range rows {
		c := strings.TrimSpace(row.Company)
		var canon string
		if c == "" {
			canon = config.GestionDefaultCompany()
		} else {
			var ok bool
			canon, ok = config.CanonicalGestionCompany(c)
			if !ok {
				return fmt.Errorf("empresa no válida: %q", c)
			}
		}
		if k := normEmail(row.Email); k != "" {
			companyStoreData.N8NByEmail[k] = canon
		}
	}
	return saveCompanyStoreLocked()
}

// PersistN8NInviteCompanies guarda la misma empresa para varios emails (formulario de gestión).
func PersistN8NInviteCompanies(emails []string, company string) error {
	rows := make([]N8NEmailCompanyRow, 0, len(emails))
	for _, e := range emails {
		rows = append(rows, N8NEmailCompanyRow{Email: e, Company: company})
	}
	return PersistN8NEmailCompanies(rows)
}
