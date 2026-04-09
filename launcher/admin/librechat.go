package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"launcher/config"
	emailpkg "launcher/email"
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
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	Role               string    `json:"role"`
	Company            string    `json:"company,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	VerificationStatus string    `json:"verificationStatus,omitempty"`
	Attempts           int       `json:"attempts,omitempty"`
	MaxAttempts        int       `json:"maxAttempts,omitempty"`
	CanRetry           bool      `json:"canRetry,omitempty"`
	RemainingAttempts  int       `json:"remainingAttempts,omitempty"`
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

		verifStatus := "verified"
		canRetry := false
		remainingAttempts := 0
		attempts := 0
		maxAttempts := 0

		if !u.EmailVerified {
			verifStatus = "pending"
			if state, err := emailpkg.GetVerificationState(u.Email); err == nil {
				verifStatus = string(state.Status)
				attempts = state.Attempts
				maxAttempts = state.MaxAttempts
				remainingAttempts = state.MaxAttempts - state.Attempts
				canRetry = state.Status != emailpkg.StatusVerified && state.Status != emailpkg.StatusBlocked && remainingAttempts > 0
			}
		}

		result = append(result, lcUserPublic{
			Email:              u.Email,
			Name:               u.Name,
			Role:               u.Role,
			Company:            co,
			CreatedAt:          u.CreatedAt,
			VerificationStatus: verifStatus,
			Attempts:           attempts,
			MaxAttempts:        maxAttempts,
			CanRetry:           canRetry,
			RemainingAttempts:  remainingAttempts,
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

type lcUserResult struct {
	Email   string `json:"email"`
	Created bool   `json:"created"`
	Error   string `json:"error,omitempty"`
}

var (
	verificationPollMu     sync.Mutex
	verificationPollingNow = map[string]bool{}
)

func createLibreChatUsersInternal(requests []createUserRequest) ([]lcUserResult, error) {
	if config.MongoURI == "" {
		return nil, fmt.Errorf("MongoDB not configured")
	}

	if len(requests) == 0 {
		return nil, fmt.Errorf("empty user list")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	results := make([]lcUserResult, 0, len(requests))

	for _, req := range requests {
		if req.Email == "" {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "email is required"})
			continue
		}
		if req.Password == "" {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "password is required"})
			continue
		}

		role := "USER"
		if req.Role != "" && req.Role != "USER" {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "solo se permite crear usuarios con rol USER"})
			continue
		}

		count, err := coll.CountDocuments(ctx, bson.M{"email": req.Email})
		if err != nil {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "failed to check existing user: " + err.Error()})
			continue
		}
		if count > 0 {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "email already exists"})
			continue
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
		if err != nil {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "failed to hash password: " + err.Error()})
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
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "empresa no válida"})
			continue
		}
		if canon, ok := CanonicalGestionCompany(co); ok {
			co = canon
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
			EmailVerified: false,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		_, err = coll.InsertOne(ctx, u)
		if err != nil {
			results = append(results, lcUserResult{Email: req.Email, Created: false, Error: "failed to insert user: " + err.Error()})
			continue
		}
		if err := SyncLibreChatUserProviderKeys(ctx, client, u.ID, co); err != nil {
			log.Printf("gestion: sincronizar keys LibreChat para %s: %v", req.Email, err)
		}

		log.Printf("DEBUG: usuario creado en MongoDB: %s (empresa: %s)", req.Email, co)

		if err := emailpkg.SendVerificationEmail(req.Email, req.Password, name); err != nil {
			log.Printf("gestion: error enviando email de verificación a %s: %v", req.Email, err)
			results = append(results, lcUserResult{Email: req.Email, Created: true, Error: "usuario creado pero error en verificación de email"})
			continue
		}

		state, stateErr := emailpkg.GetVerificationState(req.Email)
		if stateErr == nil && state.Status == emailpkg.StatusVerified {
			if err := updateUserEmailVerified(req.Email, true); err != nil {
				log.Printf("gestion: no se pudo marcar emailVerified=true para %s: %v", req.Email, err)
			}
			BroadcastVerificationUpdate(req.Email, "verified", state)
		} else {
			if stateErr == nil {
				BroadcastVerificationUpdate(req.Email, "pending", state)
			}
			go StartVerificationPolling(req.Email)
		}

		results = append(results, lcUserResult{Email: req.Email, Created: true})
	}

	return results, nil
}

func createLibreChatUsers(w http.ResponseWriter, r *http.Request) {
	var requests []createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&requests); err != nil {
		jsonError(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	results, err := createLibreChatUsersInternal(requests)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
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

	passwordNotificationQueued := false
	if reqBody.Password != "" {
		notificationBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Cambio de contraseña</title>
    <style>
        body { font-family: "Plus Jakarta Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f8fafc; margin: 0; padding: 28px 16px; }
        .wrap { max-width: 520px; margin: 0 auto; background: #ffffff; border: 1px solid #e2e8f0; border-radius: 14px; overflow: hidden; }
        .hd { background: #0f172a; color: #ffffff; padding: 20px; font-size: 18px; font-weight: 700; }
        .bd { padding: 22px; color: #0f172a; line-height: 1.55; }
        .muted { color: #64748b; font-size: 14px; }
    </style>
</head>
<body>
    <div class="wrap">
        <div class="hd">Cambio de contraseña</div>
        <div class="bd">
            <p>Hola %s,</p>
            <p>Te informamos que un administrador cambió la contraseña de tu cuenta de LibreChat.</p>
            <p>Si no reconoces este cambio, contacta al administrador de tu plataforma.</p>
            <p class="muted">Cuenta: %s</p>
        </div>
    </div>
</body>
</html>`, reqBody.Name, reqBody.Email)
		queued, mailErr := emailpkg.SendEmailWhenVerified(reqBody.Email, "Cambio de contraseña en tu cuenta", notificationBody)
		if mailErr != nil {
			log.Printf("gestion: error enviando notificación cambio contraseña a %s: %v", reqBody.Email, mailErr)
		} else {
			passwordNotificationQueued = queued
		}
	}

	var u lcUser
	if err := coll.FindOne(ctx, bson.M{"email": reqBody.Email}).Decode(&u); err == nil {
		co := strings.TrimSpace(u.Company)
		if co == "" {
			co = GestionDefaultCompany()
		}
		if err := SyncLibreChatUserProviderKeys(ctx, client, u.ID, co); err != nil {
			log.Printf("gestion: sync keys LibreChat tras actualizar %s: %v", reqBody.Email, err)
		}
	}

	jsonOK(w, map[string]interface{}{"updated": reqBody.Email, "queued": passwordNotificationQueued})
}

func StartVerificationPolling(email string) {
	verificationPollMu.Lock()
	if verificationPollingNow[email] {
		verificationPollMu.Unlock()
		return
	}
	verificationPollingNow[email] = true
	verificationPollMu.Unlock()
	defer func() {
		verificationPollMu.Lock()
		delete(verificationPollingNow, email)
		verificationPollMu.Unlock()
	}()

	pollInterval := time.Duration(config.VerificationPollInterval) * time.Minute
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	maxAttempts := config.VerificationMaxAttempts
	attempts := 0

	for {
		<-ticker.C
		attempts++

		log.Printf("SSE: checking verification status for %s (attempt %d/%d)", email, attempts, maxAttempts)

		state, err := emailpkg.CheckAndUpdateVerification(email)
		if err != nil {
			log.Printf("SSE: error checking verification for %s: %v", email, err)
			continue
		}

		switch state.Status {
		case emailpkg.StatusVerified:
			log.Printf("SSE: email verified for %s, sending credentials", email)

			password := ""
			if state.EncryptedPassword != "" {
				password, _ = emailpkg.DecryptPassword(state.EncryptedPassword)
			}
			sendCredentialsEmail(email, password, state.Name)
			updateUserEmailVerified(email, true)
			BroadcastVerificationUpdate(email, "verified", state)
			return

		case emailpkg.StatusBlocked:
			log.Printf("SSE: email blocked for %s", email)
			BroadcastVerificationUpdate(email, "blocked", state)
			return

		default:
			if attempts >= maxAttempts {
				log.Printf("SSE: max attempts reached for %s, blocking", email)
				state.Status = emailpkg.StatusBlocked
				state.ErrorMessage = "Max attempts reached"
				BroadcastVerificationUpdate(email, "blocked", state)
				return
			}
			BroadcastVerificationUpdate(email, "pending", state)
		}
	}
}

func sendCredentialsEmail(email, password, name string) error {
	emailBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Tu cuenta ha sido creada</title>
    <style>
        body {
            font-family: "Plus Jakarta Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
            background: linear-gradient(165deg, #f8fafc 0%%, #e2e8f0 100%%);
            margin: 0;
            padding: 40px 20px;
        }
        .container {
            max-width: 500px;
            margin: 0 auto;
            background: #ffffff;
            border-radius: 16px;
            box-shadow: 0 4px 24px rgba(15, 23, 42, 0.07);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(90deg, #2563eb, #8b5cf6);
            padding: 32px 24px;
            text-align: center;
        }
        .header-title {
            color: #ffffff;
            font-size: 24px;
            font-weight: 700;
            margin: 0;
        }
        .content {
            padding: 32px 24px;
        }
        .greeting {
            color: #0f172a;
            font-size: 18px;
            margin-bottom: 24px;
        }
        .card {
            background: #f8fafc;
            border-radius: 10px;
            padding: 20px;
            margin: 20px 0;
        }
        .label {
            color: #64748b;
            font-size: 13px;
            font-weight: 600;
            text-transform: uppercase;
            margin-bottom: 4px;
        }
        .value {
            color: #0f172a;
            font-size: 16px;
            font-weight: 500;
        }
        .footer {
            padding: 24px;
            text-align: center;
            color: #64748b;
            font-size: 14px;
            border-top: 1px solid #e2e8f0;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1 class="header-title">Cuenta creada</h1>
        </div>
        <div class="content">
            <p class="greeting">Hola %s,</p>
            <p>Tu cuenta ha sido creada exitosamente. Ya puedes acceder a LibreChat.</p>
            
            <div class="card">
                <div class="label">Usuario</div>
                <div class="value">%s</div>
            </div>
            
            <div class="card">
                <div class="label">Contraseña</div>
                <div class="value">%s</div>
            </div>
        </div>
        <div class="footer">
            <p>Saludos,<br>El equipo de AI Ecosystem</p>
        </div>
    </div>
</body>
</html>`, name, email, password)

	return emailpkg.SendEmail(email, "Tus credenciales de acceso", emailBody)
}

func updateUserEmailVerified(email string, verified bool) error {
	if config.MongoURI == "" {
		return fmt.Errorf("MongoDB not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	coll, client, err := mongoCollection(ctx)
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	filter := bson.M{"email": email}
	update := bson.M{"$set": bson.M{
		"emailVerified": verified,
		"updatedAt":     time.Now(),
	}}

	_, err = coll.UpdateOne(ctx, filter, update)
	return err
}

type VerificationUpdate struct {
	Email       string `json:"email"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"maxAttempts"`
	Message     string `json:"message,omitempty"`
}

func BroadcastVerificationUpdate(email, status string, state emailpkg.VerificationState) {
	update := VerificationUpdate{
		Email:       email,
		Status:      status,
		Attempts:    state.Attempts,
		MaxAttempts: state.MaxAttempts,
		Message:     state.ErrorMessage,
	}
	globalBroadcaster.Broadcast("verification_update", update)
}

func RetryVerification(email string) error {
	state, err := emailpkg.GetVerificationState(email)
	if err != nil {
		return err
	}

	if state.Status == emailpkg.StatusVerified {
		return fmt.Errorf("email already verified")
	}

	if state.Status == emailpkg.StatusBlocked {
		return fmt.Errorf("email is blocked, cannot retry")
	}

	if state.Attempts >= state.MaxAttempts {
		return fmt.Errorf("max attempts reached")
	}

	newState, err := emailpkg.RetryVerificationEmail(email)
	if err != nil {
		return err
	}

	if newState.Status == emailpkg.StatusVerified {
		if err := updateUserEmailVerified(email, true); err != nil {
			log.Printf("gestion: no se pudo marcar emailVerified=true tras retry para %s: %v", email, err)
		}
		BroadcastVerificationUpdate(email, "verified", newState)
		return nil
	}

	BroadcastVerificationUpdate(email, "pending", newState)
	go StartVerificationPolling(email)

	return nil
}
