package admin

import (
	"context"
	"fmt"
	"log"
	"time"
)

func SyncSkillToLibreChat(ctx context.Context, skill Skill, company string) error {
	groupID, err := ensureGroupForCompanyMongo(ctx, company)
	if err != nil {
		return fmt.Errorf("SyncSkillToLibreChat: ensure group: %w", err)
	}

	adminID, err := getAdminUserIDMongo(ctx)
	if err != nil {
		return fmt.Errorf("SyncSkillToLibreChat: get admin: %w", err)
	}

	exists, err := aclEntryExistsMongo(ctx, groupID, skill.SourceID)
	if err != nil {
		return fmt.Errorf("SyncSkillToLibreChat: check acl exists: %w", err)
	}

	if !exists {
		err = createAclEntryMongo(ctx, groupID, skill.SourceID, 1, adminID)
		if err != nil {
			return fmt.Errorf("SyncSkillToLibreChat: create acl entry: %w", err)
		}
	}

	if skill.Type == SkillTypeMCP && len(skill.McpServerNames) > 0 {
		for _, mcpName := range skill.McpServerNames {
			err = addMcpServerNameToAgent(ctx, skill.SourceID, mcpName)
			if err != nil {
				log.Printf("SyncSkillToLibreChat: add mcp server name %s to agent: %v", mcpName, err)
			}
		}
	}

	return nil
}

func UnsyncSkillFromCompany(ctx context.Context, skill Skill, company string) error {
	groupID, ok := GetCompanyGroupID(company)
	if !ok {
		return nil
	}

	err := deleteAclEntryMongo(ctx, groupID, skill.SourceID)
	if err != nil {
		return fmt.Errorf("UnsyncSkillFromCompany: delete acl entry: %w", err)
	}

	if skill.Type == SkillTypeMCP && len(skill.McpServerNames) > 0 {
		for _, mcpName := range skill.McpServerNames {
			err = removeMcpServerNameFromAgent(ctx, skill.SourceID, mcpName)
			if err != nil {
				log.Printf("UnsyncSkillFromCompany: remove mcp server name %s from agent: %v", mcpName, err)
			}
		}
	}

	return nil
}

func SyncUserToCompany(ctx context.Context, userID, company string) error {
	groupID, err := ensureGroupForCompanyMongo(ctx, company)
	if err != nil {
		return fmt.Errorf("SyncUserToCompany: ensure group: %w", err)
	}

	err = addGroupMembersMongo(ctx, groupID, []string{userID})
	if err != nil {
		return fmt.Errorf("SyncUserToCompany: add member: %w", err)
	}

	return nil
}

func SyncUserFromCompany(ctx context.Context, userID, company string) error {
	groupID, ok := GetCompanyGroupID(company)
	if !ok {
		return nil
	}

	err := removeGroupMembersMongo(ctx, groupID, []string{userID})
	if err != nil {
		return fmt.Errorf("SyncUserFromCompany: remove member: %w", err)
	}

	return nil
}

func SyncAllUsersForCompany(ctx context.Context, company string) error {
	userIDs, err := getUserIDsForCompanyMongo(ctx, company)
	if err != nil {
		return fmt.Errorf("SyncAllUsersForCompany: get user ids: %w", err)
	}

	groupID, err := ensureGroupForCompanyMongo(ctx, company)
	if err != nil {
		return fmt.Errorf("SyncAllUsersForCompany: ensure group: %w", err)
	}

	if len(userIDs) > 0 {
		err = addGroupMembersMongo(ctx, groupID, userIDs)
		if err != nil {
			return fmt.Errorf("SyncAllUsersForCompany: add members: %w", err)
		}
	}

	return nil
}

func SyncGroupMembersOnUserChange(ctx context.Context, userID, oldCompany, newCompany string) error {
	if oldCompany != "" {
		err := SyncUserFromCompany(ctx, userID, oldCompany)
		if err != nil {
			log.Printf("SyncGroupMembersOnUserChange: sync from old company %s: %v", oldCompany, err)
		}
	}

	if newCompany != "" {
		err := SyncUserToCompany(ctx, userID, newCompany)
		if err != nil {
			return fmt.Errorf("SyncGroupMembersOnUserChange: sync to new company %s: %w", newCompany, err)
		}
	}

	return nil
}

func DeleteCompanyGroup(ctx context.Context, company string) error {
	groupID, ok := GetCompanyGroupID(company)
	if !ok {
		return nil
	}

	err := deleteAclEntriesByPrincipalMongo(ctx, "group", groupID)
	if err != nil {
		log.Printf("DeleteCompanyGroup: delete acl entries: %v", err)
	}

	err = deleteGroupMongo(ctx, groupID)
	if err != nil {
		return fmt.Errorf("DeleteCompanyGroup: delete group: %w", err)
	}

	return nil
}

func DeleteCompanySkills(ctx context.Context, company string) error {
	skillIDs, ok := GetCompanySkillIDs(company)
	if !ok {
		return nil
	}

	for _, skillID := range skillIDs {
		skill, found := GetSkillByID(skillID)
		if !found {
			continue
		}

		err := UnsyncSkillFromCompany(ctx, skill, company)
		if err != nil {
			log.Printf("DeleteCompanySkills: unsync skill %s: %v", skillID, err)
		}
	}

	return nil
}

func CleanupAclEntries(ctx context.Context, company string) error {
	groupID, ok := GetCompanyGroupID(company)
	if !ok {
		return nil
	}

	err := deleteAclEntriesByPrincipalMongo(ctx, "group", groupID)
	if err != nil {
		return fmt.Errorf("CleanupAclEntries: %w", err)
	}

	return nil
}

func CreatePublicSkill(ctx context.Context, skill Skill) error {
	adminID, err := getAdminUserIDMongo(ctx)
	if err != nil {
		return fmt.Errorf("CreatePublicSkill: get admin: %w", err)
	}

	err = createPublicAclEntryMongo(ctx, skill.SourceID, 1, adminID)
	if err != nil {
		return fmt.Errorf("CreatePublicSkill: create acl entry: %w", err)
	}

	return nil
}

func RemovePublicSkill(ctx context.Context, skill Skill) error {
	return nil
}

func GoSyncCompanySkills(companyName string) {
	name := companyName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := SyncAllUsersForCompany(ctx, name)
		if err != nil {
			log.Printf("skills: sync en segundo plano empresa %q: %v", name, err)
		}
	}()
}
