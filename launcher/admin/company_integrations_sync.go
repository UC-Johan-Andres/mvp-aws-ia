package admin

import (
	"context"
	"fmt"
	"log"
	"time"
)

// SyncCompanyAIIntegrations propaga las API keys del perfil de empresa a todos los usuarios
// LibreChat (colección keys) y n8n (credenciales en proyecto personal) de esa empresa.
func SyncCompanyAIIntegrations(ctx context.Context, companyName string) (libreChatCount, n8nCount int, err error) {
	canon, ok := CanonicalGestionCompany(companyName)
	if !ok {
		return 0, 0, fmt.Errorf("empresa no encontrada: %q", companyName)
	}

	lc, errLC := SyncLibreChatAllUsersForCompany(ctx, canon)
	if errLC != nil {
		log.Printf("gestión: sync LibreChat empresa %q: %v", canon, errLC)
		err = errLC
	}
	libreChatCount = lc

	n8n, errN := SyncN8NAllUsersForCompany(ctx, canon)
	if errN != nil {
		log.Printf("gestión: sync n8n empresa %q: %v", canon, errN)
		if err == nil {
			err = errN
		}
	}
	n8nCount = n8n

	if err == nil {
		log.Printf("gestión: integración IA sincronizada para empresa %q (LibreChat: %d usuario(s), n8n: %d usuario(s))", canon, lc, n8n)
	}
	return libreChatCount, n8nCount, err
}

// GoSyncCompanyAIIntegrations ejecuta la sincronización en una goroutine (p. ej. tras PATCH de credenciales).
func GoSyncCompanyAIIntegrations(companyName string) {
	name := companyName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _, err := SyncCompanyAIIntegrations(ctx, name)
		if err != nil {
			log.Printf("gestión: sync en segundo plano empresa %q: %v", name, err)
		}
	}()
}
