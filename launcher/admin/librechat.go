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
	Company       string             `bson:"company,omitempty"`
	Provider      string             `bson:"provider"`
	EmailVerified bool               `bson:"emailVerified"`
	CreatedAt     time.Time          `bson:"createdAt"`
	UpdatedAt     time.Time          `bson:"updatedAt"`
}

type lcUserPublic struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Company   string    `json:"company,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func mongoCollection(ctx context.Context) (*mongo.Collection, *mongo.Client, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(config.MongoURI))
	if err != nil {
		return nil, nil, err
	}
	coll := client.Database(config.LibreChatMongoDatabase()).Collection("users")
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

	defCo := GestionDefaultCompany()
	result := make([]lcUserPublic, 0, len(users))
	for _, u := range users {
		co := strings.TrimSpace(u.Company)
		if co == "" {
			co = defCo
		}
		result = append(result, lcUserPublic{
			Email:     u.Email,
			Name:      u.Name,
			Role:      u.Role,
			Company:   co,
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
	Company  string `json:"company"`
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

		role := "USER"
		if req.Role != "" && req.Role != "USER" {
			results = append(results, result{Email: req.Email, Created: false, Error: "solo se permite crear usuarios con rol USER"})
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

		co := strings.TrimSpace(req.Company)
		if co == "" {
			co = GestionDefaultCompany()
		}
		if !IsValidGestionCompany(co) {
			results = append(results, result{Email: req.Email, Created: false, Error: "empresa no válida"})
			continue
		}

		now := time.Now()
		u := lcUser{
			ID:            primitive.NewObjectID(),
			Name:          name,
			Username:      username,
			Email:         req.Email,
			Password:      string(hash),
			Role:          role,
			Company:       co,
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

	type deleteRequest struct {
		Email string `json:"email"`
	}
	var reqBody deleteRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if reqBody.Email == "" {
		jsonError(w, "email is required", http.StatusBadRequest)
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

	res, err := coll.DeleteOne(ctx, bson.M{"email": reqBody.Email})
	if err != nil {
		jsonError(w, "failed to delete user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if res.DeletedCount == 0 {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	jsonOK(w, map[string]string{"deleted": reqBody.Email})
}

type librechatUpdateRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Company  string `json:"company"`
	Password string `json:"password,omitempty"`
}

func updateLibreChatUser(w http.ResponseWriter, r *http.Request) {
	if config.MongoURI == "" {
		jsonError(w, "MongoDB not configured", http.StatusServiceUnavailable)
		return
	}

	var reqBody librechatUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if reqBody.Email == "" {
		jsonError(w, "email is required", http.StatusBadRequest)
		return
	}

	if reqBody.Role != "" && reqBody.Role != "USER" && reqBody.Role != "ADMIN" {
		jsonError(w, "invalid role: must be USER or ADMIN", http.StatusBadRequest)
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

	update := bson.M{}
	if reqBody.Name != "" {
		update["name"] = reqBody.Name
	}
	if reqBody.Role != "" {
		update["role"] = reqBody.Role
	}
	if reqBody.Company != "" {
		if !IsValidGestionCompany(reqBody.Company) {
			jsonError(w, "empresa no válida", http.StatusBadRequest)
			return
		}
		canon, _ := CanonicalGestionCompany(reqBody.Company)
		update["company"] = canon
	}
	if reqBody.Password != "" {
		if len(reqBody.Password) < 8 {
			jsonError(w, "la contraseña debe tener al menos 8 caracteres", http.StatusBadRequest)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(reqBody.Password), 12)
		if err != nil {
			jsonError(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		update["password"] = string(hash)
	}

	if len(update) == 0 {
		jsonError(w, "no fields to update", http.StatusBadRequest)
		return
	}
	update["updatedAt"] = time.Now()

	res, err := coll.UpdateOne(ctx, bson.M{"email": reqBody.Email}, bson.M{"$set": update})
	if err != nil {
		jsonError(w, "failed to update user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if res.MatchedCount == 0 {
		jsonError(w, "user not found", http.StatusNotFound)
		return
	}

	jsonOK(w, map[string]string{"updated": reqBody.Email})
}
