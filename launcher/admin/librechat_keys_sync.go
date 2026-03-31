package admin

import (
	"context"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"launcher/config"
)

// SyncLibreChatUserProviderKeys escribe en la colección "keys" de LibreChat las API keys
// definidas en el perfil de la empresa (proveedores con LibreChatKeyName no vacío).
func SyncLibreChatUserProviderKeys(ctx context.Context, client *mongo.Client, userID primitive.ObjectID, companyField string) error {
	if client == nil || config.MongoURI == "" {
		return nil
	}
	canon, ok := CanonicalGestionCompany(companyField)
	if !ok {
		return nil
	}
	db := client.Database(config.LibreChatMongoDatabase())
	keyColl := db.Collection("keys")

	for _, p := range RegisteredProviders() {
		if p.LibreChatKeyName == "" {
			continue
		}
		apiKey, has := CompanyProviderCredentialForSync(canon, p.ID)
		if !has || apiKey == "" {
			_, _ = keyColl.DeleteOne(ctx, bson.M{"userId": userID, "name": p.LibreChatKeyName})
			continue
		}
		encVal, err := encryptLibreChatKeyValue(apiKey)
		if err != nil {
			return fmt.Errorf("keys %q (cifrado CREDS_*): %w", p.LibreChatKeyName, err)
		}
		_, err = keyColl.UpdateOne(ctx,
			bson.M{"userId": userID, "name": p.LibreChatKeyName},
			bson.M{"$set": bson.M{"userId": userID, "name": p.LibreChatKeyName, "value": encVal}},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			return fmt.Errorf("keys %q: %w", p.LibreChatKeyName, err)
		}
	}
	return nil
}

// SyncLibreChatAllUsersForCompany aplica el perfil de credenciales de la empresa a todos los usuarios LibreChat con ese company.
func SyncLibreChatAllUsersForCompany(ctx context.Context, companyCanon string) (int, error) {
	if config.MongoURI == "" || strings.TrimSpace(companyCanon) == "" {
		return 0, nil
	}
	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)

	cur, err := coll.Find(ctx, bson.M{"company": companyCanon})
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)

	n := 0
	for cur.Next(ctx) {
		var u struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		if err := cur.Decode(&u); err != nil {
			continue
		}
		if err := SyncLibreChatUserProviderKeys(ctx, client, u.ID, companyCanon); err != nil {
			return n, err
		}
		n++
	}
	return n, cur.Err()
}
