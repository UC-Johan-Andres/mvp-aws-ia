package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"launcher/config"
)

type lcUser struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	Name          string             `bson:"name"`
	Username      string             `bson:"username"`
	Email         string             `bson:"email"`
	Password      string             `bson:"password"`
	Role          string             `bson:"role"`
	Provider      string             `bson:"provider"`
	EmailVerified bool               `bson:"emailVerified"`
	CreatedAt     time.Time          `bson:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt"`
}

type lcUserPublic struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

func mongoCollection(ctx context.Context) (*mongo.Collection, *mongo.Client, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(config.MongoURI))
	if err != nil {
		return nil, nil, err
	}
	coll := client.Database("LibreChat").Collection("users")
	return coll, client, nil
}

func listLibreChatUsers(w http.ResponseWriter, r *http.Request) {
	if config.MongoURI == "" {
		jsonError(w, "MongoDB not configured", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		jsonError(w, "failed to connect to MongoDB: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer client.Disconnect(ctx)

	cursor, err := coll.Find(ctx, bson.D{})
	if err != nil {
		jsonError(w, "failed to query users: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var users []lcUser
	if err := cursor.All(ctx, &users); err != nil {
		jsonError(w, "failed to decode users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]lcUserPublic, 0, len(users))
	for _, u := range users {
		result = append(result, lcUserPublic{
			Email:     u.Email,
			Name:      u.Name,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		})
	}

	jsonOK(w, result)
}

type createUserRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func createLibreChatUsers(w http.ResponseWriter, r *http.Request) {
	if config.MongoURI == "" {
		jsonError(w, "MongoDB not configured", http.StatusServiceUnavailable)
		return
	}

	var requests []createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if len(requests) == 0 {
		jsonError(w, "empty user list", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		jsonError(w, "failed to connect to MongoDB: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer client.Disconnect(ctx)

	type result struct {
		Email   string `json:"email"`
		Created bool   `json:"created"`
		Error   string `json:"error,omitempty"`
	}

	results := make([]result, 0, len(requests))

	for _, req := range requests {
		if req.Email == "" {
			results = append(results, result{Email: req.Email, Created: false, Error: "email is required"})
			continue
		}
		if req.Password == "" {
			results = append(results, result{Email: req.Email, Created: false, Error: "password is required"})
			continue
		}

		role := req.Role
		if role == "" {
			role = "USER"
		}
		if role != "USER" && role != "ADMIN" {
			results = append(results, result{Email: req.Email, Created: false, Error: "invalid role, must be USER or ADMIN"})
			continue
		}

		// Check for existing user
		count, err := coll.CountDocuments(ctx, bson.M{"email": req.Email})
		if err != nil {
			results = append(results, result{Email: req.Email, Created: false, Error: "failed to check existing user: " + err.Error()})
			continue
		}
		if count > 0 {
			results = append(results, result{Email: req.Email, Created: false, Error: "email already exists"})
			continue
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
		if err != nil {
			results = append(results, result{Email: req.Email, Created: false, Error: "failed to hash password: " + err.Error()})
			continue
		}

		username := req.Email
		if idx := strings.Index(req.Email, "@"); idx >= 0 {
			username = req.Email[:idx]
		}

		name := req.Name
		if name == "" {
			name = username
		}

		now := time.Now()
		u := lcUser{
			ID:            primitive.NewObjectID(),
			Name:          name,
			Username:      username,
			Email:         req.Email,
			Password:      string(hash),
			Role:          role,
			Provider:      "local",
			EmailVerified: true,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		_, err = coll.InsertOne(ctx, u)
		if err != nil {
			results = append(results, result{Email: req.Email, Created: false, Error: "failed to insert user: " + err.Error()})
			continue
		}

		results = append(results, result{Email: req.Email, Created: true})
	}

	jsonOK(w, results)
}

func deleteLibreChatUser(w http.ResponseWriter, r *http.Request) {
	if config.MongoURI == "" {
		jsonError(w, "MongoDB not configured", http.StatusServiceUnavailable)
		return
	}

	email := r.PathValue("email")
	if email == "" {
		jsonError(w, "email path parameter is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		jsonError(w, "failed to connect to MongoDB: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer client.Disconnect(ctx)

	res, err := coll.DeleteOne(ctx, bson.M{"email": email})
	if err != nil {
		jsonError(w, "failed to delete user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if res.DeletedCount == 0 {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	jsonOK(w, map[string]string{"deleted": email})
}
