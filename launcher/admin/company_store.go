package admin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"launcher/config"
)

// CompanyProfile credenciales por proveedor por empresa (extensible).
type CompanyProfile struct {
	Credentials map[string]ProviderCredential `json:"credentials,omitempty"`
}

type companyStoreFile struct {
	Companies        []string                   `json:"companies"`
	DefaultCompany   string                     `json:"defaultCompany"`
	CompanyProfiles  map[string]CompanyProfile  `json:"companyProfiles,omitempty"`
	N8NByEmail       map[string]string          `json:"n8nByEmail"`
	N8NByID          map[string]string          `json:"n8nByID"`
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
	if companyStoreData.CompanyProfiles == nil {
		companyStoreData.CompanyProfiles = make(map[string]CompanyProfile)
	}
}

func parseCompaniesFromEnvRaw() []string {
	raw := strings.TrimSpace(os.Getenv("GESTION_COMPANIES"))
	if raw == "" {
		raw = "default"
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		out = []string{"default"}
	}
	return out
}

func pickDefaultFromEnv(list []string) string {
	d := strings.TrimSpace(os.Getenv("GESTION_DEFAULT_COMPANY"))
	if d == "" {
		return list[0]
	}
	for _, c := range list {
		if strings.EqualFold(c, d) {
			return c
		}
	}
	return list[0]
}

func normalizeCompaniesFileLocked() {
	if len(companyStoreData.Companies) == 0 {
		companyStoreData.Companies = parseCompaniesFromEnvRaw()
	}
	companyStoreData.DefaultCompany = strings.TrimSpace(companyStoreData.DefaultCompany)
	if companyStoreData.DefaultCompany == "" {
		companyStoreData.DefaultCompany = pickDefaultFromEnv(companyStoreData.Companies)
	}
	if _, ok := indexCompanyInsensitive(companyStoreData.Companies, companyStoreData.DefaultCompany); !ok {
		companyStoreData.DefaultCompany = companyStoreData.Companies[0]
	}
}

// defaultCompanyUnlocked: usar solo con companyStoreMu ya bloqueado (RLock o Lock).
func defaultCompanyUnlocked() string {
	if len(companyStoreData.Companies) == 0 {
		return "default"
	}
	d := strings.TrimSpace(companyStoreData.DefaultCompany)
	for _, c := range companyStoreData.Companies {
		if strings.EqualFold(c, d) {
			return c
		}
	}
	return companyStoreData.Companies[0]
}

func loadCompanyStoreFromDisk() error {
	path := config.GestionCompanyStorePath()
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()

	ensureCompanyMaps()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			companyStoreData.Companies = parseCompaniesFromEnvRaw()
			companyStoreData.DefaultCompany = pickDefaultFromEnv(companyStoreData.Companies)
			companyStoreLoaded = true
			return saveCompanyStoreLocked()
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
	normalizeCompaniesFileLocked()
	pruneOrphanCompanyProfilesLocked()
	companyStoreLoaded = true
	return saveCompanyStoreLocked()
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
	normalizeCompaniesFileLocked()
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

// InitCompanyStore carga o crea el JSON de empresas y asignaciones n8n.
func InitCompanyStore() error {
	return loadCompanyStoreFromDisk()
}

func normEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// GestionCompaniesList devuelve la lista de empresas (copia).
func GestionCompaniesList() []string {
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded || len(companyStoreData.Companies) == 0 {
		return []string{"default"}
	}
	out := make([]string, len(companyStoreData.Companies))
	copy(out, companyStoreData.Companies)
	return out
}

// GestionDefaultCompany devuelve la empresa predeterminada.
func GestionDefaultCompany() string {
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded || len(companyStoreData.Companies) == 0 {
		return "default"
	}
	d := strings.TrimSpace(companyStoreData.DefaultCompany)
	for _, c := range companyStoreData.Companies {
		if strings.EqualFold(c, d) {
			return c
		}
	}
	return companyStoreData.Companies[0]
}

// IsValidGestionCompany indica si el nombre está en el registro.
func IsValidGestionCompany(name string) bool {
	_, ok := CanonicalGestionCompany(name)
	return ok
}

// CanonicalGestionCompany devuelve el nombre canónico del registro.
func CanonicalGestionCompany(name string) (string, bool) {
	s := strings.TrimSpace(name)
	if s == "" {
		return "", false
	}
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded {
		return "", false
	}
	for _, c := range companyStoreData.Companies {
		if strings.EqualFold(c, s) {
			return c, true
		}
	}
	return "", false
}

func indexCompanyInsensitive(list []string, name string) (int, bool) {
	for i, c := range list {
		if strings.EqualFold(c, name) {
			return i, true
		}
	}
	return 0, false
}

// pruneOrphanCompanyProfilesLocked elimina perfiles cuyo nombre ya no está en Companies.
func pruneOrphanCompanyProfilesLocked() {
	if companyStoreData.CompanyProfiles == nil {
		return
	}
	valid := make(map[string]bool, len(companyStoreData.Companies))
	for _, c := range companyStoreData.Companies {
		valid[c] = true
	}
	for key := range companyStoreData.CompanyProfiles {
		if !valid[key] {
			delete(companyStoreData.CompanyProfiles, key)
		}
	}
}

// CompanyProfileMasked es la forma segura para API/UI (sin secretos completos).
type CompanyProfileMasked struct {
	Credentials map[string]ProviderCredentialMasked `json:"credentials,omitempty"`
}

// ProviderCredentialMasked info por proveedor para la UI.
type ProviderCredentialMasked struct {
	APIKey       string `json:"apiKey,omitempty"`
	APIKeyMasked string `json:"apiKeyMasked,omitempty"`
	Configured   bool   `json:"configured"`
}

// CompanyProfileMaskedForName devuelve perfil enmascarado; vacío si no hay perfil.
func CompanyProfileMaskedForName(companyCanon string) CompanyProfileMasked {
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded || companyCanon == "" {
		return CompanyProfileMasked{}
	}
	p, ok := companyStoreData.CompanyProfiles[companyCanon]
	if !ok || len(p.Credentials) == 0 {
		return CompanyProfileMasked{}
	}
	out := CompanyProfileMasked{Credentials: make(map[string]ProviderCredentialMasked)}
	for prov, c := range p.Credentials {
		k := strings.TrimSpace(c.APIKey)
		if k == "" {
			continue
		}
		out.Credentials[prov] = ProviderCredentialMasked{
			APIKey:       k,
			APIKeyMasked: MaskAPIKey(k),
			Configured:   true,
		}
	}
	return out
}

// CompanyProviderCredentialForSync devuelve apiKey guardada para empresa+proveedor.
func CompanyProviderCredentialForSync(companyName, providerID string) (string, bool) {
	canon, ok := CanonicalGestionCompany(companyName)
	if !ok {
		return "", false
	}
	pid, err := NormalizeProviderID(providerID)
	if err != nil {
		return "", false
	}
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded {
		return "", false
	}
	p, ok := companyStoreData.CompanyProfiles[canon]
	if !ok {
		return "", false
	}
	c, ok := p.Credentials[pid]
	if !ok {
		return "", false
	}
	k := strings.TrimSpace(c.APIKey)
	if k == "" {
		return "", false
	}
	return k, true
}

func validateNewCompanyName(name string) error {
	s := strings.TrimSpace(name)
	if s == "" {
		return fmt.Errorf("el nombre no puede estar vacío")
	}
	if len(s) > 64 {
		return fmt.Errorf("máximo 64 caracteres")
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("solo letras, números, espacio, guión, guión bajo y punto")
	}
	return nil
}

// AddGestionCompany añade una empresa al registro.
func AddGestionCompany(name string) error {
	return AddGestionCompanyWithCredentials(name, nil)
}

// AddGestionCompanyWithCredentials añade empresa y opcionalmente credenciales por proveedor.
func AddGestionCompanyWithCredentials(name string, creds map[string]ProviderCredential) error {
	if err := validateNewCompanyName(name); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	normCreds, err := NormalizeCredentialsMapInput(creds)
	if err != nil {
		return err
	}
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	normalizeCompaniesFileLocked()
	if _, ok := indexCompanyInsensitive(companyStoreData.Companies, name); ok {
		return fmt.Errorf("ya existe una empresa con ese nombre")
	}
	companyStoreData.Companies = append(companyStoreData.Companies, name)
	sort.Slice(companyStoreData.Companies, func(i, j int) bool {
		return strings.ToLower(companyStoreData.Companies[i]) < strings.ToLower(companyStoreData.Companies[j])
	})
	idx, _ := indexCompanyInsensitive(companyStoreData.Companies, name)
	canon := companyStoreData.Companies[idx]
	if len(normCreds) > 0 {
		companyStoreData.CompanyProfiles[canon] = CompanyProfile{Credentials: normCreds}
	}
	return saveCompanyStoreLocked()
}

// SetGestionDefaultCompany marca la empresa predeterminada.
func SetGestionDefaultCompany(name string) error {
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	normalizeCompaniesFileLocked()
	idx, ok := indexCompanyInsensitive(companyStoreData.Companies, name)
	if !ok {
		return fmt.Errorf("empresa no encontrada")
	}
	companyStoreData.DefaultCompany = companyStoreData.Companies[idx]
	return saveCompanyStoreLocked()
}

// RenameGestionCompany renombra en el registro y en mapas n8n locales. mongoRenameCompany debe llamarse aparte.
func RenameGestionCompany(oldName, newName string) (oldCanon, newCanon string, err error) {
	if err := validateNewCompanyName(newName); err != nil {
		return "", "", err
	}
	newName = strings.TrimSpace(newName)
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	normalizeCompaniesFileLocked()
	idx, ok := indexCompanyInsensitive(companyStoreData.Companies, oldName)
	if !ok {
		return "", "", fmt.Errorf("empresa no encontrada")
	}
	if _, dup := indexCompanyInsensitive(companyStoreData.Companies, newName); dup {
		return "", "", fmt.Errorf("ya existe una empresa con el nombre destino")
	}
	oldCanon = companyStoreData.Companies[idx]
	newCanon = newName
	companyStoreData.Companies[idx] = newCanon
	if strings.EqualFold(companyStoreData.DefaultCompany, oldCanon) {
		companyStoreData.DefaultCompany = newCanon
	}
	for k, v := range companyStoreData.N8NByEmail {
		if strings.EqualFold(v, oldCanon) {
			companyStoreData.N8NByEmail[k] = newCanon
		}
	}
	for k, v := range companyStoreData.N8NByID {
		if strings.EqualFold(v, oldCanon) {
			companyStoreData.N8NByID[k] = newCanon
		}
	}
	sort.Slice(companyStoreData.Companies, func(i, j int) bool {
		return strings.ToLower(companyStoreData.Companies[i]) < strings.ToLower(companyStoreData.Companies[j])
	})
	if companyStoreData.CompanyProfiles != nil {
		if prof, ok := companyStoreData.CompanyProfiles[oldCanon]; ok {
			delete(companyStoreData.CompanyProfiles, oldCanon)
			companyStoreData.CompanyProfiles[newCanon] = prof
		}
	}
	return oldCanon, newCanon, saveCompanyStoreLocked()
}

// ReassignTargetForCompanyRemoval devuelve la empresa canónica del store a la que deben moverse usuarios
// (LibreChat en Mongo y entradas n8n en JSON) al eliminar la empresa identificada por `canon`
// (nombre canónico de GestionCompaniesList). Misma regla que DeleteGestionCompany para n8n.
func ReassignTargetForCompanyRemoval(canon string) (reassign string, err error) {
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	ensureCompanyMaps()
	if len(companyStoreData.Companies) <= 1 {
		return "", fmt.Errorf("debe quedar al menos una empresa")
	}
	if _, ok := indexCompanyInsensitive(companyStoreData.Companies, canon); !ok {
		return "", fmt.Errorf("empresa no encontrada")
	}
	reassign = defaultCompanyUnlocked()
	if strings.EqualFold(reassign, canon) {
		reassign = ""
		for _, c := range companyStoreData.Companies {
			if !strings.EqualFold(c, canon) {
				reassign = c
				break
			}
		}
		if reassign == "" {
			return "", fmt.Errorf("no hay empresa destino para reasignar usuarios")
		}
	}
	return reassign, nil
}

// DeleteGestionCompany elimina una empresa. Los usuarios n8n del almacén local pasan a la predeterminada
// u otra superviviente si se borra la que era default. Los usuarios LibreChat deben actualizarse en Mongo antes (companies_api).
func DeleteGestionCompany(name string) error {
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	normalizeCompaniesFileLocked()
	if len(companyStoreData.Companies) <= 1 {
		return fmt.Errorf("debe quedar al menos una empresa")
	}
	idx, ok := indexCompanyInsensitive(companyStoreData.Companies, name)
	if !ok {
		return fmt.Errorf("empresa no encontrada")
	}
	canon := companyStoreData.Companies[idx]

	// A qué empresa canónica mover los n8n que apuntaban a `canon`.
	reassign := defaultCompanyUnlocked()
	if strings.EqualFold(reassign, canon) {
		reassign = ""
		for _, c := range companyStoreData.Companies {
			if !strings.EqualFold(c, canon) {
				reassign = c
				break
			}
		}
		if reassign == "" {
			return fmt.Errorf("no hay empresa destino para reasignar usuarios n8n")
		}
	}
	for k, v := range companyStoreData.N8NByEmail {
		if strings.EqualFold(v, canon) {
			companyStoreData.N8NByEmail[k] = reassign
		}
	}
	for k, v := range companyStoreData.N8NByID {
		if strings.EqualFold(v, canon) {
			companyStoreData.N8NByID[k] = reassign
		}
	}

	companyStoreData.Companies = append(companyStoreData.Companies[:idx], companyStoreData.Companies[idx+1:]...)
	if strings.EqualFold(companyStoreData.DefaultCompany, canon) {
		companyStoreData.DefaultCompany = companyStoreData.Companies[0]
	}
	if companyStoreData.CompanyProfiles != nil {
		delete(companyStoreData.CompanyProfiles, canon)
	}
	return saveCompanyStoreLocked()
}

// MergeGestionCompanyCredentials fusiona credenciales en el perfil de una empresa existente.
func MergeGestionCompanyCredentials(companyName string, patch map[string]ProviderCredential) error {
	normPatch, err := NormalizeCredentialsMapInput(patch)
	if err != nil {
		return err
	}
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	normalizeCompaniesFileLocked()
	idx, ok := indexCompanyInsensitive(companyStoreData.Companies, companyName)
	if !ok {
		return fmt.Errorf("empresa no encontrada")
	}
	canon := companyStoreData.Companies[idx]
	prof := companyStoreData.CompanyProfiles[canon]
	if prof.Credentials == nil {
		prof.Credentials = make(map[string]ProviderCredential)
	}
	merged := MergeCredentialsMaps(prof.Credentials, normPatch)
	// También permitir borrar con patch explícito: NormalizeCredentialsMapInput omite vacíos;
	// MergeCredentialsMaps necesita entradas vacías para borrar — añadimos paso para keys en patch con apiKey "".
	for k, v := range patch {
		nid, e := NormalizeProviderID(k)
		if e != nil {
			continue
		}
		if strings.TrimSpace(v.APIKey) == "" {
			delete(merged, nid)
		}
	}
	if len(merged) == 0 {
		delete(companyStoreData.CompanyProfiles, canon)
	} else {
		companyStoreData.CompanyProfiles[canon] = CompanyProfile{Credentials: merged}
	}
	return saveCompanyStoreLocked()
}

// N8NUserCompany devuelve la empresa asociada a un usuario n8n (archivo local).
func N8NUserCompany(userID, email string) string {
	companyStoreMu.RLock()
	defer companyStoreMu.RUnlock()
	if !companyStoreLoaded {
		return defaultCompanyUnlocked()
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
	return defaultCompanyUnlocked()
}

// SetN8NUserCompany guarda empresa para n8n (por email y opcionalmente por id).
func SetN8NUserCompany(userID, email, company string) error {
	canon, ok := CanonicalGestionCompany(company)
	if !ok {
		return fmt.Errorf("empresa no válida: %q", company)
	}
	companyStoreMu.Lock()
	defer companyStoreMu.Unlock()
	ensureCompanyMaps()
	if k := normEmail(email); k != "" {
		companyStoreData.N8NByEmail[k] = canon
	}
	if id := strings.TrimSpace(userID); id != "" {
		companyStoreData.N8NByID[id] = canon
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
	normalizeCompaniesFileLocked()
	for _, row := range rows {
		c := strings.TrimSpace(row.Company)
		var canon string
		if c == "" {
			canon = defaultCompanyUnlocked()
		} else {
			var ok bool
			canon, ok = canonicalCompanyUnlocked(c)
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

func canonicalCompanyUnlocked(name string) (string, bool) {
	s := strings.TrimSpace(name)
	if s == "" {
		return "", false
	}
	for _, c := range companyStoreData.Companies {
		if strings.EqualFold(c, s) {
			return c, true
		}
	}
	return "", false
}

// PersistN8NInviteCompanies guarda la misma empresa para varios emails (formulario de gestión).
func PersistN8NInviteCompanies(emails []string, company string) error {
	rows := make([]N8NEmailCompanyRow, 0, len(emails))
	for _, e := range emails {
		rows = append(rows, N8NEmailCompanyRow{Email: e, Company: company})
	}
	return PersistN8NEmailCompanies(rows)
}
