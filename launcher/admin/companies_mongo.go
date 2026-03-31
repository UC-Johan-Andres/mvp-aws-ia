package admin

import (
	"context"
	"regexp"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"launcher/config"
)

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

// libreChatUserIDsWithCompany devuelve los _id de usuarios cuya empresa coincide con companyCanon (insensible a mayúsculas).
func libreChatUserIDsWithCompany(ctx context.Context, companyCanon string) ([]primitive.ObjectID, error) {
	if config.MongoURI == "" {
		return nil, nil
	}
	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)
	pattern := "^" + regexp.QuoteMeta(companyCanon) + "$"
	cursor, err := coll.Find(ctx,
		bson.M{"company": bson.M{"$regex": pattern, "$options": "i"}},
		options.Find().SetProjection(bson.M{"_id": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]primitive.ObjectID, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	return ids, nil
}

func mongoSetLibreChatUsersCompanyByIDs(ctx context.Context, ids []primitive.ObjectID, companyCanon string) (int64, error) {
	if config.MongoURI == "" || len(ids) == 0 {
		return 0, nil
	}
	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return 0, err
	}
	defer client.Disconnect(ctx)
	res, err := coll.UpdateMany(ctx,
		bson.M{"_id": bson.M{"$in": ids}},
		bson.M{"$set": bson.M{"company": companyCanon}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
