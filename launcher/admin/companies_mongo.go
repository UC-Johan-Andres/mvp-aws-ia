package admin

import (
	"context"
	"regexp"

	"go.mongodb.org/mongo-driver/bson"

	"launcher/config"
)

func countLibreChatUsersWithCompany(ctx context.Context, companyCanon string) (int64, error) {
	if config.MongoURI == "" {
		return 0, nil
	}
	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)
	pattern := "^" + regexp.QuoteMeta(companyCanon) + "$"
	return coll.CountDocuments(ctx, bson.M{
		"company": bson.M{"$regex": pattern, "$options": "i"},
	})
}

func mongoRenameCompanyOnUsers(ctx context.Context, oldCanon, newCanon string) (int64, error) {
	if config.MongoURI == "" {
		return 0, nil
	}
	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)
	pattern := "^" + regexp.QuoteMeta(oldCanon) + "$"
	res, err := coll.UpdateMany(ctx,
		bson.M{"company": bson.M{"$regex": pattern, "$options": "i"}},
		bson.M{"$set": bson.M{"company": newCanon}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
