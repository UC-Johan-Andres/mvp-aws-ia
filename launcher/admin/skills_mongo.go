package admin

import (
	"context"
	"fmt"
	"time"

	"launcher/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func librechatMongoDB() string {
	return config.LibreChatMongoDatabase()
}

func skillsMongoCollection(ctx context.Context, collectionName string) (*mongo.Collection, *mongo.Client, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(config.MongoURI))
	if err != nil {
		return nil, nil, err
	}
	coll := client.Database(librechatMongoDB()).Collection(collectionName)
	return coll, client, nil
}

func createGroupMongo(ctx context.Context, name string) (*primitive.ObjectID, error) {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	now := time.Now()
	doc := bson.M{
		"name":        name,
		"description": "",
		"email":       "",
		"memberIds":   []string{},
		"source":      "local",
		"tenantId":    "",
		"createdAt":   now,
		"updatedAt":   now,
	}

	result, err := coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("createGroupMongo: %w", err)
	}

	id := result.InsertedID.(primitive.ObjectID)
	return &id, nil
}

func getGroupByNameMongo(ctx context.Context, name string) (bson.M, error) {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	var group bson.M
	err = coll.FindOne(ctx, bson.M{"name": name}).Decode(&group)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return group, nil
}

func listGroupsMongo(ctx context.Context) ([]bson.M, error) {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	cursor, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var groups []bson.M
	if err := cursor.All(ctx, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func addGroupMembersMongo(ctx context.Context, groupID string, userIDs []string) error {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return fmt.Errorf("invalid groupID: %w", err)
	}

	_, err = coll.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$addToSet": bson.M{"memberIds": bson.M{"$each": userIDs}}},
	)
	if err != nil {
		return fmt.Errorf("addGroupMembersMongo: %w", err)
	}
	return nil
}

func removeGroupMembersMongo(ctx context.Context, groupID string, userIDs []string) error {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return fmt.Errorf("invalid groupID: %w", err)
	}

	_, err = coll.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$pull": bson.M{"memberIds": bson.M{"$in": userIDs}}},
	)
	if err != nil {
		return fmt.Errorf("removeGroupMembersMongo: %w", err)
	}
	return nil
}

func getGroupMembersMongo(ctx context.Context, groupID string) ([]string, error) {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return nil, fmt.Errorf("invalid groupID: %w", err)
	}

	var group bson.M
	err = coll.FindOne(ctx, bson.M{"_id": objID}).Decode(&group)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	memberIds, ok := group["memberIds"].(primitive.A)
	if !ok {
		return []string{}, nil
	}

	result := make([]string, 0, len(memberIds))
	for _, m := range memberIds {
		if s, ok := m.(string); ok {
			result = append(result, s)
		}
	}
	return result, nil
}

func deleteGroupMongo(ctx context.Context, groupID string) error {
	coll, client, err := skillsMongoCollection(ctx, "groups")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return fmt.Errorf("invalid groupID: %w", err)
	}

	_, err = coll.DeleteOne(ctx, bson.M{"_id": objID})
	if err != nil {
		return fmt.Errorf("deleteGroupMongo: %w", err)
	}
	return nil
}

func createAclEntryMongo(ctx context.Context, groupID, agentID string, permBits int, grantedBy string) error {
	coll, client, err := skillsMongoCollection(ctx, "aclentries")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	groupObjID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return fmt.Errorf("invalid groupID: %w", err)
	}

	agentObjID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return fmt.Errorf("invalid agentID: %w", err)
	}

	grantedByObjID, err := primitive.ObjectIDFromHex(grantedBy)
	if err != nil {
		return fmt.Errorf("invalid grantedBy: %w", err)
	}

	doc := bson.M{
		"principalType":  "group",
		"principalId":    groupObjID,
		"principalModel": "Group",
		"resourceType":   "agent",
		"resourceId":     agentObjID,
		"permBits":       permBits,
		"grantedBy":      grantedByObjID,
		"grantedAt":      time.Now(),
	}

	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("createAclEntryMongo: %w", err)
	}
	return nil
}

func createPublicAclEntryMongo(ctx context.Context, agentID string, permBits int, grantedBy string) error {
	coll, client, err := skillsMongoCollection(ctx, "aclentries")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	agentObjID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return fmt.Errorf("invalid agentID: %w", err)
	}

	grantedByObjID, err := primitive.ObjectIDFromHex(grantedBy)
	if err != nil {
		return fmt.Errorf("invalid grantedBy: %w", err)
	}

	doc := bson.M{
		"principalType": "public",
		"resourceType":  "agent",
		"resourceId":    agentObjID,
		"permBits":      permBits,
		"grantedBy":     grantedByObjID,
		"grantedAt":     time.Now(),
	}

	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		return fmt.Errorf("createPublicAclEntryMongo: %w", err)
	}
	return nil
}

func deleteAclEntryMongo(ctx context.Context, groupID, agentID string) error {
	coll, client, err := skillsMongoCollection(ctx, "aclentries")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	groupObjID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return fmt.Errorf("invalid groupID: %w", err)
	}

	agentObjID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return fmt.Errorf("invalid agentID: %w", err)
	}

	_, err = coll.DeleteOne(ctx, bson.M{
		"principalType": "group",
		"principalId":   groupObjID,
		"resourceType":  "agent",
		"resourceId":    agentObjID,
	})
	if err != nil {
		return fmt.Errorf("deleteAclEntryMongo: %w", err)
	}
	return nil
}

func deleteAclEntriesByPrincipalMongo(ctx context.Context, principalType, principalID string) error {
	coll, client, err := skillsMongoCollection(ctx, "aclentries")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(principalID)
	if err != nil {
		return fmt.Errorf("invalid principalID: %w", err)
	}

	_, err = coll.DeleteMany(ctx, bson.M{
		"principalType": principalType,
		"principalId":   objID,
	})
	if err != nil {
		return fmt.Errorf("deleteAclEntriesByPrincipalMongo: %w", err)
	}
	return nil
}

func aclEntryExistsMongo(ctx context.Context, groupID, agentID string) (bool, error) {
	coll, client, err := skillsMongoCollection(ctx, "aclentries")
	if err != nil {
		return false, err
	}
	defer client.Disconnect(ctx)

	groupObjID, err := primitive.ObjectIDFromHex(groupID)
	if err != nil {
		return false, fmt.Errorf("invalid groupID: %w", err)
	}

	agentObjID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return false, fmt.Errorf("invalid agentID: %w", err)
	}

	count, err := coll.CountDocuments(ctx, bson.M{
		"principalType": "group",
		"principalId":   groupObjID,
		"resourceType":  "agent",
		"resourceId":    agentObjID,
	})
	if err != nil {
		return false, fmt.Errorf("aclEntryExistsMongo: %w", err)
	}
	return count > 0, nil
}

func listAgentsMongo(ctx context.Context) ([]bson.M, error) {
	coll, client, err := skillsMongoCollection(ctx, "agents")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	cursor, err := coll.Find(ctx, bson.M{"archived": bson.M{"$ne": true}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var agents []bson.M
	if err := cursor.All(ctx, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

func getAgentByIDMongo(ctx context.Context, agentID string) (bson.M, error) {
	coll, client, err := skillsMongoCollection(ctx, "agents")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return nil, fmt.Errorf("invalid agentID: %w", err)
	}

	var agent bson.M
	err = coll.FindOne(ctx, bson.M{"_id": objID}).Decode(&agent)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return agent, nil
}

func getUserIDsForCompanyMongo(ctx context.Context, company string) ([]string, error) {
	coll, client, err := skillsMongoCollection(ctx, "users")
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	cursor, err := coll.Find(ctx, bson.M{"company": company}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	userIDs := make([]string, 0, len(results))
	for _, r := range results {
		userIDs = append(userIDs, r.ID)
	}
	return userIDs, nil
}

func getAdminUserIDMongo(ctx context.Context) (string, error) {
	coll, client, err := skillsMongoCollection(ctx, "users")
	if err != nil {
		return "", err
	}
	defer client.Disconnect(ctx)

	var user bson.M
	err = coll.FindOne(ctx, bson.M{"role": "ADMIN"}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", fmt.Errorf("no admin user found")
		}
		return "", err
	}

	if id, ok := user["_id"].(primitive.ObjectID); ok {
		return id.Hex(), nil
	}
	return "", fmt.Errorf("admin user _id is not ObjectID")
}

func ensureGroupForCompanyMongo(ctx context.Context, company string) (string, error) {
	group, err := getGroupByNameMongo(ctx, company)
	if err != nil {
		return "", err
	}

	if group != nil {
		if id, ok := group["_id"].(primitive.ObjectID); ok {
			return id.Hex(), nil
		}
	}

	newGroupID, err := createGroupMongo(ctx, company)
	if err != nil {
		return "", err
	}

	return newGroupID.Hex(), nil
}

func updateAgentMcpServerNames(ctx context.Context, agentID string, mcpServerNames []string) error {
	coll, client, err := skillsMongoCollection(ctx, "agents")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return fmt.Errorf("invalid agentID: %w", err)
	}

	_, err = coll.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$set": bson.M{"mcpServerNames": mcpServerNames, "updatedAt": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("updateAgentMcpServerNames: %w", err)
	}
	return nil
}

func addMcpServerNameToAgent(ctx context.Context, agentID, mcpServerName string) error {
	coll, client, err := skillsMongoCollection(ctx, "agents")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return fmt.Errorf("invalid agentID: %w", err)
	}

	_, err = coll.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$addToSet": bson.M{"mcpServerNames": mcpServerName}, "$set": bson.M{"updatedAt": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("addMcpServerNameToAgent: %w", err)
	}
	return nil
}

func removeMcpServerNameFromAgent(ctx context.Context, agentID, mcpServerName string) error {
	coll, client, err := skillsMongoCollection(ctx, "agents")
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	objID, err := primitive.ObjectIDFromHex(agentID)
	if err != nil {
		return fmt.Errorf("invalid agentID: %w", err)
	}

	_, err = coll.UpdateOne(ctx,
		bson.M{"_id": objID},
		bson.M{"$pull": bson.M{"mcpServerNames": mcpServerName}, "$set": bson.M{"updatedAt": time.Now()}},
	)
	if err != nil {
		return fmt.Errorf("removeMcpServerNameFromAgent: %w", err)
	}
	return nil
}
