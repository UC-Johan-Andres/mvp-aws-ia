package admin

import (
	"context"
	"fmt"
	"log"
	"strings"
)

func MigrateExistingUsersToGroups(ctx context.Context) error {
	if IsMigrationCompleted() {
		log.Printf("skills-migration: ya completada, omitir")
		return nil
	}

	log.Printf("skills-migration: iniciando migracion de usuarios existentes a Groups")

	companies := GestionCompaniesList()
	log.Printf("skills-migration: empresas a migrar: %v", companies)

	for _, company := range companies {
		if strings.TrimSpace(company) == "" {
			continue
		}

		userIDs, err := getUserIDsForCompanyMongo(ctx, company)
		if err != nil {
			log.Printf("skills-migration: error obtener usuarios de empresa %s: %v", company, err)
			continue
		}

		log.Printf("skills-migration: empresa %s tiene %d usuarios", company, len(userIDs))

		groupID, err := ensureGroupForCompanyMongo(ctx, company)
		if err != nil {
			log.Printf("skills-migration: error crear/obtener grupo para empresa %s: %v", company, err)
			continue
		}

		log.Printf("skills-migration: grupo %s para empresa %s", groupID, company)

		if len(userIDs) > 0 {
			err = addGroupMembersMongo(ctx, groupID, userIDs)
			if err != nil {
				log.Printf("skills-migration: error agregar usuarios al grupo %s: %v", groupID, err)
				continue
			}
			log.Printf("skills-migration: %d usuarios agregados al grupo de empresa %s", len(userIDs), company)
		}

		err = SaveCompanyGroupID(company, groupID)
		if err != nil {
			log.Printf("skills-migration: error guardar groupID para empresa %s: %v", company, err)
			continue
		}
	}

	err := MarkMigrationCompleted()
	if err != nil {
		return fmt.Errorf("skills-migration: error marcar migracion como completada: %w", err)
	}

	log.Printf("skills-migration: migracion completada exitosamente")
	return nil
}

func RunMigrationsIfNeeded(ctx context.Context) error {
	if IsMigrationCompleted() {
		return nil
	}

	log.Printf("skills-migration: ejecutando migraciones pendientes...")

	err := MigrateExistingUsersToGroups(ctx)
	if err != nil {
		return fmt.Errorf("skills-migration: run migrations if needed: %w", err)
	}

	return nil
}

func MigrationAlreadyRun() bool {
	return IsMigrationCompleted()
}

func InitSkillStoreWithMigration(ctx context.Context) error {
	err := InitSkillStore()
	if err != nil {
		return fmt.Errorf("InitSkillStoreWithMigration: init skill store: %w", err)
	}

	if !IsMigrationCompleted() {
		log.Printf("InitSkillStoreWithMigration: migracion pendiente, ejecutando...")
		err = MigrateExistingUsersToGroups(ctx)
		if err != nil {
			log.Printf("InitSkillStoreWithMigration: error en migracion: %v", err)
		}
	}

	return nil
}

func ForceReRunMigration(ctx context.Context) error {
	err := InitSkillStore()
	if err != nil {
		return fmt.Errorf("ForceReRunMigration: init skill store: %w", err)
	}

	companies := GestionCompaniesList()
	log.Printf("ForceReRunMigration: forzando re-migracion para %d empresas", len(companies))

	for _, company := range companies {
		userIDs, err := getUserIDsForCompanyMongo(ctx, company)
		if err != nil {
			log.Printf("ForceReRunMigration: error obtener usuarios de empresa %s: %v", company, err)
			continue
		}

		groupID, err := ensureGroupForCompanyMongo(ctx, company)
		if err != nil {
			log.Printf("ForceReRunMigration: error grupo para empresa %s: %v", company, err)
			continue
		}

		if len(userIDs) > 0 {
			err = addGroupMembersMongo(ctx, groupID, userIDs)
			if err != nil {
				log.Printf("ForceReRunMigration: error agregar usuarios al grupo %s: %v", groupID, err)
				continue
			}
		}

		err = SaveCompanyGroupID(company, groupID)
		if err != nil {
			log.Printf("ForceReRunMigration: error guardar groupID para empresa %s: %v", company, err)
		}
	}

	err = MarkMigrationCompleted()
	if err != nil {
		return fmt.Errorf("ForceReRunMigration: mark migration completed: %w", err)
	}

	log.Printf("ForceReRunMigration: re-migracion completada")
	return nil
}
