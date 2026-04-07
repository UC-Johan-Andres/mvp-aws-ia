package email

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	appconfig "launcher/config"
)

const aesBlockSize = 16

func aesNewCipher(key []byte) (cipher.Block, error) {
	return aes.NewCipher(key)
}

func NewCBCEncrypter(block cipher.Block, iv []byte) cipher.BlockMode {
	return cipher.NewCBCEncrypter(block, iv)
}

func NewCBCDecrypter(block cipher.Block, iv []byte) cipher.BlockMode {
	return cipher.NewCBCDecrypter(block, iv)
}

func encryptPassword(plaintext string) (string, error) {
	keyHex := strings.TrimSpace(os.Getenv("CREDS_KEY"))
	ivHex := strings.TrimSpace(os.Getenv("CREDS_IV"))
	if keyHex == "" || ivHex == "" {
		return "", fmt.Errorf("CREDS_KEY y CREDS_IV deben estar definidos")
	}

	key, err := hexDecode(keyHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_KEY no es hex válido: %w", err)
	}
	iv, err := hexDecode(ivHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_IV no es hex válido: %w", err)
	}

	block, err := aesNewCipher(key)
	if err != nil {
		return "", err
	}

	plaintextBytes := []byte(plaintext)
	padding := aesBlockSize - len(plaintextBytes)%aesBlockSize
	padded := make([]byte, len(plaintextBytes)+padding)
	copy(padded, plaintextBytes)
	for i := len(plaintextBytes); i < len(padded); i++ {
		padded[i] = byte(padding)
	}

	ciphertext := make([]byte, len(padded))
	NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return hexEncode(ciphertext), nil
}

func DecryptPassword(hexCipher string) (string, error) {
	keyHex := strings.TrimSpace(os.Getenv("CREDS_KEY"))
	ivHex := strings.TrimSpace(os.Getenv("CREDS_IV"))
	key, err := hexDecode(keyHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_KEY: %w", err)
	}
	iv, err := hexDecode(ivHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_IV: %w", err)
	}

	ciphertext, err := hexDecode(strings.TrimSpace(hexCipher))
	if err != nil {
		return "", err
	}

	block, err := aesNewCipher(key)
	if err != nil {
		return "", err
	}

	plain := make([]byte, len(ciphertext))
	NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)

	padding := int(plain[len(plain)-1])
	if padding > aesBlockSize || padding == 0 {
		return "", fmt.Errorf("padding inválido")
	}

	for i := len(plain) - padding; i < len(plain); i++ {
		if plain[i] != byte(padding) {
			return "", fmt.Errorf("padding incoherente")
		}
	}

	return string(plain[:len(plain)-padding]), nil
}

func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("string hex debe tener longitud par")
	}
	result := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		high := s[i]
		low := s[i+1]
		var v byte
		if high >= '0' && high <= '9' {
			v = (high - '0') << 4
		} else if high >= 'a' && high <= 'f' {
			v = (high - 'a' + 10) << 4
		} else if high >= 'A' && high <= 'F' {
			v = (high - 'A' + 10) << 4
		} else {
			return nil, fmt.Errorf("char hex inválido: %c", high)
		}
		if low >= '0' && low <= '9' {
			v += low - '0'
		} else if low >= 'a' && low <= 'f' {
			v += low - 'a' + 10
		} else if low >= 'A' && low <= 'F' {
			v += low - 'A' + 10
		} else {
			return nil, fmt.Errorf("char hex inválido: %c", low)
		}
		result[i/2] = v
	}
	return result, nil
}

func hexEncode(data []byte) string {
	hexChars := "0123456789abcdef"
	result := make([]byte, len(data)*2)
	for i, b := range data {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}
	return string(result)
}

func StartVerificationWorkerPool(workers int) {
	verificationQueue = make(chan string, 100)

	for i := 0; i < workers; i++ {
		go verificationWorker(i)
	}
}

func QueueVerification(email string) {
	if verificationQueue == nil {
		processVerification(email)
		return
	}
	verificationQueue <- email
}

func verificationWorker(id int) {
	for email := range verificationQueue {
		pollingMu.Lock()
		if activePolling[email] {
			pollingMu.Unlock()
			continue
		}
		activePolling[email] = true
		pollingMu.Unlock()

		processVerification(email)

		pollingMu.Lock()
		delete(activePolling, email)
		pollingMu.Unlock()
	}
}

func processVerification(email string) {
	state, err := CheckAndUpdateVerification(email)
	if err != nil {
		return
	}

	if state.Status == StatusVerified {
		password := ""
		if state.EncryptedPassword != "" {
			password, _ = DecryptPassword(state.EncryptedPassword)
		}
		if password != "" {
			sendEmailWithCredentials(email, password, state.Name)
		}
	}
}

func sendEmailWithCredentials(email, password, name string) error {
	emailBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Tu cuenta ha sido creada</title>
    <style>
        body { font-family: "Plus Jakarta Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: linear-gradient(165deg, #f8fafc 0%%, #e2e8f0 100%%); margin: 0; padding: 40px 20px; }
        .container { max-width: 500px; margin: 0 auto; background: #ffffff; border-radius: 16px; box-shadow: 0 4px 24px rgba(15, 23, 42, 0.07); overflow: hidden; }
        .header { background: linear-gradient(90deg, #2563eb, #8b5cf6); padding: 32px 24px; text-align: center; }
        .header-title { color: #ffffff; font-size: 24px; font-weight: 700; margin: 0; }
        .content { padding: 32px 24px; }
        .greeting { color: #0f172a; font-size: 18px; margin-bottom: 24px; }
        .card { background: #f8fafc; border-radius: 10px; padding: 20px; margin: 20px 0; }
        .label { color: #64748b; font-size: 13px; font-weight: 600; text-transform: uppercase; margin-bottom: 4px; }
        .value { color: #0f172a; font-size: 16px; font-weight: 500; }
        .footer { padding: 24px; text-align: center; color: #64748b; font-size: 14px; border-top: 1px solid #e2e8f0; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header"><h1 class="header-title">Cuenta creada</h1></div>
        <div class="content">
            <p class="greeting">Hola %s,</p>
            <p>Tu cuenta ha sido creada exitosamente. Ya puedes acceder a LibreChat.</p>
            <div class="card"><div class="label">Usuario</div><div class="value">%s</div></div>
            <div class="card"><div class="label">Contraseña</div><div class="value">%s</div></div>
        </div>
        <div class="footer"><p>Saludos,<br>El equipo de AI Ecosystem</p></div>
    </div>
</body>
</html>`, name, email, password)

	return SendEmail(email, "Tus credenciales de acceso", emailBody)
}

type VerificationStatus string

const (
	StatusPending  VerificationStatus = "pending"
	StatusVerified VerificationStatus = "verified"
	StatusBlocked  VerificationStatus = "blocked"
	StatusError    VerificationStatus = "error"
)

type VerificationState struct {
	Email             string             `json:"email"`
	Status            VerificationStatus `json:"status"`
	Attempts          int                `json:"attempts"`
	MaxAttempts       int                `json:"max_attempts"`
	LastCheck         time.Time          `json:"last_check"`
	CreatedAt         time.Time          `json:"created_at"`
	ErrorMessage      string             `json:"error_message,omitempty"`
	EncryptedPassword string             `json:"encrypted_password,omitempty"`
	Name              string             `json:"name,omitempty"`
}

type VerificationStore struct {
	States map[string]VerificationState `json:"states"`
}

var (
	sesClient         *sesv2.Client
	store             *VerificationStore
	verificationQueue chan string
	activePolling     = make(map[string]bool)
	pollingMu         sync.Mutex
)

func initSESClient() error {
	if appconfig.AWSAccessKeyID == "" || appconfig.AWSSecretAccessKey == "" {
		return fmt.Errorf("AWS credentials not configured")
	}

	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(appconfig.AWSRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			appconfig.AWSAccessKeyID,
			appconfig.AWSSecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	sesClient = sesv2.NewFromConfig(awsCfg)
	return nil
}

func LoadVerificationStore() error {
	store = &VerificationStore{
		States: make(map[string]VerificationState),
	}

	dataPath := appconfig.VerificationDataPath
	if dataPath == "" {
		dataPath = "/app/data"
	}

	filePath := filepath.Join(dataPath, "verification_states.json")

	if _, err := os.Stat(filePath); err == nil {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read verification store: %w", err)
		}
		if err := json.Unmarshal(data, store); err != nil {
			return fmt.Errorf("failed to parse verification store: %w", err)
		}
	}

	return nil
}

func SaveVerificationStore() error {
	dataPath := appconfig.VerificationDataPath
	if dataPath == "" {
		dataPath = "/app/data"
	}

	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	filePath := filepath.Join(dataPath, "verification_states.json")
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal verification store: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write verification store: %w", err)
	}

	return nil
}

func SendVerificationEmail(email, password, name string) error {
	if store == nil {
		if err := LoadVerificationStore(); err != nil {
			return err
		}
	}

	prev, hasPrev := store.States[email]

	encryptedPassword := ""
	if password != "" {
		var err error
		encryptedPassword, err = encryptPassword(password)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
	} else if hasPrev {
		encryptedPassword = prev.EncryptedPassword
	}

	if name == "" && hasPrev {
		name = prev.Name
	}

	if appconfig.AWSAccessKeyID == "" || appconfig.AWSSecretAccessKey == "" {
		return sendCredentialsDirect(email, encryptedPassword, name)
	}

	if sesClient == nil {
		if err := initSESClient(); err != nil {
			return sendCredentialsDirect(email, encryptedPassword, name)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := ensureSESIdentityStatus(ctx, email)
	if err != nil {
		log.Printf("ERROR: SES identity check failed for %s: %v", email, err)
		state := VerificationState{
			Email:             email,
			Status:            StatusError,
			Attempts:          prev.Attempts,
			MaxAttempts:       appconfig.VerificationMaxAttempts,
			CreatedAt:         prev.CreatedAt,
			LastCheck:         time.Now(),
			EncryptedPassword: encryptedPassword,
			Name:              name,
			ErrorMessage:      err.Error(),
		}
		if state.CreatedAt.IsZero() {
			state.CreatedAt = time.Now()
		}
		store.States[email] = state
		if saveErr := SaveVerificationStore(); saveErr != nil {
			log.Printf("ERROR: Failed to save verification state: %v", saveErr)
		}
		return fmt.Errorf("failed to ensure SES email identity: %w", err)
	}

	state := VerificationState{
		Email:             email,
		Status:            status,
		Attempts:          prev.Attempts,
		MaxAttempts:       appconfig.VerificationMaxAttempts,
		CreatedAt:         prev.CreatedAt,
		LastCheck:         time.Now(),
		EncryptedPassword: encryptedPassword,
		Name:              name,
	}
	if state.CreatedAt.IsZero() {
		state.CreatedAt = time.Now()
	}
	if !hasPrev || password != "" {
		state.Attempts = 0
	}
	if status == StatusVerified {
		state.ErrorMessage = ""
	}

	store.States[email] = state
	if err := SaveVerificationStore(); err != nil {
		return err
	}

	if status == StatusVerified && encryptedPassword != "" {
		password, err := DecryptPassword(encryptedPassword)
		if err == nil && password != "" {
			if err := sendEmailWithCredentials(email, password, name); err != nil {
				return fmt.Errorf("failed to send credentials email: %w", err)
			}
		}
	}

	return nil
}

func RetryVerificationEmail(email string) (VerificationState, error) {
	if store == nil {
		if err := LoadVerificationStore(); err != nil {
			return VerificationState{}, err
		}
	}

	state, exists := store.States[email]
	if !exists {
		return VerificationState{}, fmt.Errorf("email %s not found in verification store", email)
	}

	if appconfig.AWSAccessKeyID == "" || appconfig.AWSSecretAccessKey == "" {
		state.Status = StatusVerified
		state.ErrorMessage = ""
		state.LastCheck = time.Now()
		store.States[email] = state
		if err := SaveVerificationStore(); err != nil {
			return VerificationState{}, err
		}
		if state.EncryptedPassword != "" {
			password, err := DecryptPassword(state.EncryptedPassword)
			if err == nil && password != "" {
				if err := sendEmailWithCredentials(email, password, state.Name); err != nil {
					return state, fmt.Errorf("failed to send credentials email: %w", err)
				}
			}
		}
		return state, nil
	}

	if sesClient == nil {
		if err := initSESClient(); err != nil {
			return VerificationState{}, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := ensureSESIdentityStatus(ctx, email)
	if err != nil {
		state.ErrorMessage = err.Error()
		state.LastCheck = time.Now()
		store.States[email] = state
		_ = SaveVerificationStore()
		return state, err
	}

	state.Status = status
	state.LastCheck = time.Now()
	state.ErrorMessage = ""
	store.States[email] = state
	if err := SaveVerificationStore(); err != nil {
		return VerificationState{}, err
	}

	if status == StatusVerified && state.EncryptedPassword != "" {
		password, derr := DecryptPassword(state.EncryptedPassword)
		if derr == nil && password != "" {
			if err := sendEmailWithCredentials(email, password, state.Name); err != nil {
				return state, fmt.Errorf("failed to send credentials email: %w", err)
			}
		}
	}

	return state, nil
}

func sendCredentialsDirect(email, encryptedPassword, name string) error {
	if store == nil {
		if err := LoadVerificationStore(); err != nil {
			return err
		}
	}

	state := VerificationState{
		Email:             email,
		Status:            StatusVerified,
		Attempts:          0,
		MaxAttempts:       appconfig.VerificationMaxAttempts,
		CreatedAt:         time.Now(),
		LastCheck:         time.Now(),
		EncryptedPassword: encryptedPassword,
		Name:              name,
	}

	store.States[email] = state
	if err := SaveVerificationStore(); err != nil {
		return err
	}

	if encryptedPassword != "" {
		password, err := DecryptPassword(encryptedPassword)
		if err == nil && password != "" {
			if err := sendEmailWithCredentials(email, password, name); err != nil {
				return fmt.Errorf("failed to send credentials email: %w", err)
			}
		}
	}

	return nil
}

func GetVerificationStatus(email string) (VerificationStatus, error) {
	if sesClient == nil {
		if err := initSESClient(); err != nil {
			return StatusError, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := sesGetEmailIdentity(ctx, email)
	if err != nil {
		if isSESNotFoundError(err) {
			return StatusPending, nil
		}
		return StatusError, fmt.Errorf("failed to get email identity: %w", err)
	}

	if isIdentityVerified(resp) {
		return StatusVerified, nil
	}

	return StatusPending, nil
}

func sesGetEmailIdentity(ctx context.Context, email string) (*sesv2.GetEmailIdentityOutput, error) {
	return sesClient.GetEmailIdentity(ctx, &sesv2.GetEmailIdentityInput{EmailIdentity: &email})
}

func isIdentityVerified(resp *sesv2.GetEmailIdentityOutput) bool {
	if resp == nil {
		return false
	}
	if resp.DkimAttributes != nil && resp.DkimAttributes.Status == types.DkimStatusSuccess {
		return true
	}
	if resp.IdentityType == types.IdentityTypeEmailAddress && resp.VerifiedForSendingStatus {
		return true
	}
	return false
}

func ensureSESIdentityStatus(ctx context.Context, email string) (VerificationStatus, error) {
	resp, err := sesGetEmailIdentity(ctx, email)
	if err == nil {
		if isIdentityVerified(resp) {
			return StatusVerified, nil
		}
		return StatusPending, nil
	}

	if !isSESNotFoundError(err) {
		return StatusError, fmt.Errorf("get email identity: %w", err)
	}

	_, err = sesClient.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{
		EmailIdentity: &email,
	})
	if err != nil {
		if isSESAlreadyExistsError(err) {
			return StatusPending, nil
		}
		return StatusError, fmt.Errorf("create email identity: %w", err)
	}

	return StatusPending, nil
}

func isSESNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var notFound *types.NotFoundException
	if errors.As(err, &notFound) {
		return true
	}
	if isSmithyCode(err, "NotFoundException") {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not found") || strings.Contains(errStr, "does not exist")
}

func isSESAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	var alreadyExists *types.AlreadyExistsException
	if errors.As(err, &alreadyExists) {
		return true
	}
	if isSmithyCode(err, "AlreadyExistsException") {
		return true
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "already exists")
}

func isSmithyCode(err error, code string) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == code
	}
	return false
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if isSmithyCode(err, "TooManyRequestsException") || isSmithyCode(err, "ThrottlingException") {
		return true
	}
	errStr := err.Error()

	transientIndicators := []string{
		"rate limited",
		"throttling",
		"timeout",
		"deadline exceeded",
		"context canceled",
		"i/o timeout",
		"connection refused",
		"no such host",
		"temporary failure",
		"service unavailable",
	}

	errLower := strings.ToLower(errStr)
	for _, indicator := range transientIndicators {
		if strings.Contains(errLower, indicator) {
			return true
		}
	}

	return false
}

func CheckAndUpdateVerification(email string) (VerificationState, error) {
	if store == nil {
		if err := LoadVerificationStore(); err != nil {
			return VerificationState{}, err
		}
	}

	state, exists := store.States[email]
	if !exists {
		return VerificationState{}, fmt.Errorf("email %s not found in verification store", email)
	}

	if state.Status == StatusVerified || state.Status == StatusBlocked {
		return state, nil
	}

	if state.Attempts >= state.MaxAttempts {
		state.Status = StatusBlocked
		state.ErrorMessage = "Max attempts reached"
		store.States[email] = state
		SaveVerificationStore()
		return state, nil
	}

	status, err := GetVerificationStatus(email)
	if err != nil {
		if isTransientError(err) {
			state.LastCheck = time.Now()
			state.ErrorMessage = err.Error()
			store.States[email] = state
			SaveVerificationStore()
			return state, nil
		}
		state.Attempts++
		state.LastCheck = time.Now()
		state.ErrorMessage = err.Error()
		store.States[email] = state
		SaveVerificationStore()
		return state, err
	}

	state.Status = status
	state.LastCheck = time.Now()

	if status == StatusVerified {
		state.ErrorMessage = ""
	} else if status == StatusBlocked {
		state.ErrorMessage = "Email blocked by SES"
	}

	store.States[email] = state
	SaveVerificationStore()

	return state, nil
}

func CanRetry(email string) (bool, int, error) {
	if store == nil {
		if err := LoadVerificationStore(); err != nil {
			return false, 0, err
		}
	}

	state, exists := store.States[email]
	if !exists {
		return false, 0, fmt.Errorf("email %s not found in verification store", email)
	}

	remaining := state.MaxAttempts - state.Attempts
	canRetry := state.Status != StatusVerified && state.Status != StatusBlocked && remaining > 0

	return canRetry, remaining, nil
}

func GetVerificationState(email string) (VerificationState, error) {
	if store == nil {
		if err := LoadVerificationStore(); err != nil {
			return VerificationState{}, err
		}
	}

	state, exists := store.States[email]
	if !exists {
		return VerificationState{}, fmt.Errorf("email %s not found in verification store", email)
	}

	return state, nil
}
