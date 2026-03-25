package admin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"launcher/config"
)

// LibreChatStatsSeriesItem agrega conversaciones, mensajes y última actividad por usuario (email).
type LibreChatStatsSeriesItem struct {
	Email                string     `json:"email"`
	Name                 string     `json:"name"`
	Company              string     `json:"company,omitempty"`
	TotalConversations   int64      `json:"totalConversations"`
	TotalMessages        int64      `json:"totalMessages"`
	LastActivity         *time.Time `json:"lastActivity,omitempty"`
}

// LibreChatStatsTotals resume el bloque devuelto al dashboard.
type LibreChatStatsTotals struct {
	UsersWithActivity   int   `json:"usersWithActivity"`
	TotalConversations  int64 `json:"totalConversations"`
	TotalMessages       int64 `json:"totalMessages"`
}

type lcConvAggRow struct {
	Email              string `bson:"email"`
	Name               string `bson:"name"`
	TotalConversations int64  `bson:"totalConversations"`
}

type lcMsgAggRow struct {
	Email         string    `bson:"email"`
	Name          string    `bson:"name"`
	TotalMessages int64     `bson:"totalMessages"`
	LastActivity  time.Time `bson:"lastActivity"`
}

// FetchLibreChatStatsSeries ejecuta las mismas agregaciones probadas en mongosh (DB LibreChat).
func FetchLibreChatStatsSeries(ctx context.Context) ([]LibreChatStatsSeriesItem, LibreChatStatsTotals, error) {
	uri := strings.TrimSpace(config.MongoURI)
	if uri == "" {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("MONGO_URI no está configurada")
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("mongo connect: %w", err)
	}
	defer func() { _ = client.Disconnect(ctx) }()

	if err := client.Ping(ctx, nil); err != nil {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("mongo ping: %w", err)
	}

	db := client.Database(config.LibreChatMongoDatabase())
	convColl := db.Collection("conversations")
	msgColl := db.Collection("messages")

	convPipe := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.M{
			"_id":                "$user",
			"totalConversations": bson.M{"$sum": 1},
		}}},
		bson.D{{Key: "$addFields", Value: bson.M{
			"userIdObj": bson.M{"$toObjectId": "$_id"},
		}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "userIdObj",
			"foreignField": "_id",
			"as":           "userInfo",
		}}},
		bson.D{{Key: "$unwind", Value: "$userInfo"}},
		bson.D{{Key: "$project", Value: bson.M{
			"_id":                0,
			"email":              "$userInfo.email",
			"name":               "$userInfo.name",
			"totalConversations": 1,
		}}},
		bson.D{{Key: "$sort", Value: bson.M{"totalConversations": -1}}},
	}

	msgPipe := mongo.Pipeline{
		bson.D{{Key: "$group", Value: bson.M{
			"_id":            "$user",
			"totalMessages":  bson.M{"$sum": 1},
			"lastActivity":   bson.M{"$max": "$createdAt"},
		}}},
		bson.D{{Key: "$addFields", Value: bson.M{
			"userIdObj": bson.M{"$toObjectId": "$_id"},
		}}},
		bson.D{{Key: "$lookup", Value: bson.M{
			"from":         "users",
			"localField":   "userIdObj",
			"foreignField": "_id",
			"as":           "userInfo",
		}}},
		bson.D{{Key: "$unwind", Value: "$userInfo"}},
		bson.D{{Key: "$project", Value: bson.M{
			"_id":            0,
			"email":          "$userInfo.email",
			"name":           "$userInfo.name",
			"totalMessages":  1,
			"lastActivity":   1,
		}}},
		bson.D{{Key: "$sort", Value: bson.M{"totalMessages": -1}}},
	}

	cur, err := convColl.Aggregate(ctx, convPipe)
	if err != nil {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("conversations aggregate: %w", err)
	}
	defer cur.Close(ctx)
	var convRows []lcConvAggRow
	if err := cur.All(ctx, &convRows); err != nil {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("conversations decode: %w", err)
	}

	cur2, err := msgColl.Aggregate(ctx, msgPipe)
	if err != nil {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("messages aggregate: %w", err)
	}
	defer cur2.Close(ctx)
	var msgRows []lcMsgAggRow
	if err := cur2.All(ctx, &msgRows); err != nil {
		return nil, LibreChatStatsTotals{}, fmt.Errorf("messages decode: %w", err)
	}

	byEmail := make(map[string]*LibreChatStatsSeriesItem)
	key := func(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

	for _, r := range convRows {
		k := key(r.Email)
		if k == "" {
			continue
		}
		if byEmail[k] == nil {
			byEmail[k] = &LibreChatStatsSeriesItem{Email: r.Email, Name: r.Name}
		}
		byEmail[k].TotalConversations = r.TotalConversations
		if strings.TrimSpace(byEmail[k].Name) == "" {
			byEmail[k].Name = r.Name
		}
	}
	for _, r := range msgRows {
		k := key(r.Email)
		if k == "" {
			continue
		}
		if byEmail[k] == nil {
			byEmail[k] = &LibreChatStatsSeriesItem{Email: r.Email, Name: r.Name}
		}
		byEmail[k].TotalMessages = r.TotalMessages
		if !r.LastActivity.IsZero() {
			t := r.LastActivity
			if byEmail[k].LastActivity == nil || t.After(*byEmail[k].LastActivity) {
				byEmail[k].LastActivity = &t
			}
		}
		if strings.TrimSpace(byEmail[k].Name) == "" {
			byEmail[k].Name = r.Name
		}
	}

	out := make([]LibreChatStatsSeriesItem, 0, len(byEmail))
	var totals LibreChatStatsTotals
	for _, v := range byEmail {
		out = append(out, *v)
		totals.TotalConversations += v.TotalConversations
		totals.TotalMessages += v.TotalMessages
	}
	totals.UsersWithActivity = len(out)

	attachLibreChatCompanies(ctx, db, out)

	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalMessages != out[j].TotalMessages {
			return out[i].TotalMessages > out[j].TotalMessages
		}
		return out[i].TotalConversations > out[j].TotalConversations
	})

	return out, totals, nil
}

func attachLibreChatCompanies(ctx context.Context, db *mongo.Database, items []LibreChatStatsSeriesItem) {
	userColl := db.Collection("users")
	cur, err := userColl.Find(ctx, bson.D{}, options.Find().SetProjection(bson.M{"email": 1, "company": 1}))
	if err != nil {
		return
	}
	defer cur.Close(ctx)

	emailToCompany := make(map[string]string)
	for cur.Next(ctx) {
		var doc struct {
			Email   string `bson:"email"`
			Company string `bson:"company"`
		}
		if cur.Decode(&doc) != nil {
			continue
		}
		k := normEmail(doc.Email)
		if k == "" {
			continue
		}
		co := strings.TrimSpace(doc.Company)
		if co == "" {
			co = config.GestionDefaultCompany()
		}
		emailToCompany[k] = co
	}
	def := config.GestionDefaultCompany()
	for i := range items {
		k := normEmail(items[i].Email)
		if c, ok := emailToCompany[k]; ok {
			items[i].Company = c
		} else {
			items[i].Company = def
		}
	}
}
