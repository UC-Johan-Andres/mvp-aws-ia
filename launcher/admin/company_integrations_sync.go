package admin

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// SyncResult detalle de la sincronización para diagnóstico.
type SyncResult struct {
	Company        string   `json:"company"`
	CompanyFound   bool     `json:"companyFound"`
	ProvidersFound []string `json:"providersFound,omitempty"`
	LibreChat      int      `json:"libreChatUsers"`
	N8N            int      `json:"n8nUsers"`
	Errors         []string `json:"errors,omitempty"`
}

// SyncCompanyAIIntegrations propaga las API keys del perfil de empresa a todos los usuarios
// LibreChat (colección keys) y n8n (credenciales en proyecto personal) de esa empresa.
func SyncCompanyAIIntegrations(ctx context.Context, companyName string) (*SyncResult, error) {
	res := &SyncResult{Company: strings.TrimSpace(companyName)}

	canon, ok := CanonicalGestionCompany(companyName)
	if !ok {
		res.CompanyFound = false
		return res, fmt.Errorf("empresa no encontrada: %q", companyName)
	}
	res.Company = canon
	res.CompanyFound = true

	for _, p := range RegisteredProviders() {
		if _, has := CompanyProviderCredentialForSync(canon, p.ID); has {
			res.ProvidersFound = append(res.ProvidersFound, p.ID)
		}
	}
	log.Printf("gestión sync: empresa=%q proveedores=%v", canon, res.ProvidersFound)

	lc, errLC := SyncLibreChatAllUsersForCompany(ctx, canon)
	if errLC != nil {
		msg := fmt.Sprintf("LibreChat: %v", errLC)
		log.Printf("gestión sync: %s", msg)
		res.Errors = append(res.Errors, msg)
	}
	res.LibreChat = lc

	n8n, errN := SyncN8NAllUsersForCompany(ctx, canon)
	if errN != nil {
		msg := fmt.Sprintf("n8n: %v", errN)
		log.Printf("gestión sync: %s", msg)
		res.Errors = append(res.Errors, msg)
	}
	res.N8N = n8n

	log.Printf("gestión sync: empresa=%q LibreChat=%d n8n=%d errores=%d", canon, lc, n8n, len(res.Errors))

	var err error
	if errLC != nil {
		err = errLC
	}
	if errN != nil && err == nil {
		err = errN
	}
	return res, err
}

// GoSyncCompanyAIIntegrations ejecuta la sincronización en una goroutine (p. ej. tras PATCH de credenciales).
func GoSyncCompanyAIIntegrations(companyName string) {
	name := companyName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := SyncCompanyAIIntegrations(ctx, name)
		if err != nil {
			log.Printf("gestión: sync en segundo plano empresa %q: %v (resultado: %+v)", name, err, res)
		}
	}()
}
