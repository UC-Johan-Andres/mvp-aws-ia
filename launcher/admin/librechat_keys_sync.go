package admin

import (
	"context"
	"fmt"
	"log"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"launcher/config"
)

// loadLibrechatJWTUser lee username/email/provider para firmar el mismo JWT que generateToken de LibreChat.
func loadLibrechatJWTUser(ctx context.Context, client *mongo.Client, userID primitive.ObjectID) (librechatJWTUser, error) {
	var row struct {
		Username string `bson:"username"`
		Email    string `bson:"email"`
		Provider string `bson:"provider"`
	}
	usersColl := client.Database(config.LibreChatMongoDatabase()).Collection("users")
	if err := usersColl.FindOne(ctx, bson.M{"_id": userID}).Decode(&row); err != nil {
		return librechatJWTUser{}, err
	}
	return librechatJWTUser{
		IDHex:    userID.Hex(),
		Username: row.Username,
		Email:    row.Email,
		Provider: row.Provider,
	}, nil
}

// SyncLibreChatUserProviderKeys escribe en LibreChat las API keys
// definidas en el perfil de la empresa (proveedores con LibreChatKeyName no vacío).
//
// Estrategia:
//  1. Si LIBRECHAT_JWT_SECRET o JWT_SECRET coinciden con LibreChat → PUT /api/keys (LibreChat cifra).
//  2. Fallback (CREDS_KEY/CREDS_IV) → cifrado AES-CBC local + escritura Mongo directa.
func SyncLibreChatUserProviderKeys(ctx context.Context, client *mongo.Client, userID primitive.ObjectID, companyField string) error {
	if client == nil || config.MongoURI == "" {
		return nil
	}
	canon, ok := CanonicalGestionCompany(companyField)
	if !ok {
		return nil
	}

	useHTTP := librechatHTTPKeysAvailable()
	userHex := userID.Hex()

	var jwtUser librechatJWTUser
	if useHTTP {
		var err error
		jwtUser, err = loadLibrechatJWTUser(ctx, client, userID)
		if err != nil {
			return fmt.Errorf("usuario LibreChat %s para JWT (sync keys): %w", userHex, err)
		}
	}

	for _, p := range RegisteredProviders() {
		if p.LibreChatKeyName == "" {
			continue
		}
		apiKey, has := CompanyProviderCredentialForSync(canon, p.ID)
		if !has || apiKey == "" {
			if err := deleteKey(ctx, client, userID, jwtUser, p.LibreChatKeyName, useHTTP); err != nil {
				log.Printf("librechat-keys: borrar %q para %s: %v", p.LibreChatKeyName, userHex, err)
			}
			continue
		}

		if useHTTP {
			if err := putLibreChatUserKey(jwtUser, p.LibreChatKeyName, apiKey); err != nil {
				return fmt.Errorf("keys %q (HTTP): %w — comprobar JWT_SECRET=LIBRECHAT_JWT_SECRET y que LibreChat esté encendido", p.LibreChatKeyName, err)
			}
			continue
		}

		if err := upsertKeyMongo(ctx, client, userID, p.LibreChatKeyName, apiKey); err != nil {
			return err
		}
	}
	return nil
}

func deleteKey(ctx context.Context, client *mongo.Client, userID primitive.ObjectID, jwtUser librechatJWTUser, keyName string, useHTTP bool) error {
	if useHTTP {
		return deleteLibreChatUserKeyHTTP(jwtUser, keyName)
	}
	db := client.Database(config.LibreChatMongoDatabase())
	_, err := db.Collection("keys").DeleteOne(ctx, bson.M{"userId": userID, "name": keyName})
	return err
}

// upsertKeyMongo: fallback con cifrado local (CREDS_KEY/CREDS_IV).
func upsertKeyMongo(ctx context.Context, client *mongo.Client, userID primitive.ObjectID, keyName, apiKey string) error {
	encVal, err := encryptLibreChatKeyValue(apiKey)
	if err != nil {
		return fmt.Errorf("keys %q (cifrado CREDS_*): %w", keyName, err)
	}
	db := client.Database(config.LibreChatMongoDatabase())
	_, err = db.Collection("keys").UpdateOne(ctx,
		bson.M{"userId": userID, "name": keyName},
		bson.M{
			"$set":   bson.M{"userId": userID, "name": keyName, "value": encVal},
			"$unset": bson.M{"expiresAt": ""},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("keys %q: %w", keyName, err)
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
