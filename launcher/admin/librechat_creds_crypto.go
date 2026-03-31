package admin

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// encryptLibreChatKeyValue replica el método encrypt() de LibreChat (@librechat/data-schemas):
// AES-CBC, IV fijo desde CREDS_IV (hex), clave desde CREDS_KEY (hex), salida = ciphertext en hex.
// Sin esto, Mongo guarda texto plano y decrypt() falla → agentes/chat reportan no_user_key u errores opacos.
func encryptLibreChatKeyValue(plaintext string) (string, error) {
	keyHex := strings.TrimSpace(os.Getenv("CREDS_KEY"))
	ivHex := strings.TrimSpace(os.Getenv("CREDS_IV"))
	if keyHex == "" || ivHex == "" {
		return "", fmt.Errorf("CREDS_KEY y CREDS_IV deben estar definidos (mismos valores que en el contenedor librechat)")
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_KEY no es hex válido: %w", err)
	}
	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_IV no es hex válido: %w", err)
	}
	if len(iv) != aes.BlockSize {
		return "", fmt.Errorf("CREDS_IV debe decodificar a %d bytes (AES block), tiene %d", aes.BlockSize, len(iv))
	}
	klen := len(key)
	if klen != 16 && klen != 24 && klen != 32 {
		return "", fmt.Errorf("CREDS_KEY en hex debe ser 16, 24 o 32 bytes; tiene %d", klen)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	if len(padded)%aes.BlockSize != 0 {
		return "", fmt.Errorf("padding interno inválido")
	}
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)
	return hex.EncodeToString(ciphertext), nil
}

// decryptLibreChatKeyValue replica decrypt() legacy de LibreChat (solo tests y diagnóstico).
func decryptLibreChatKeyValue(hexCipher string) (string, error) {
	keyHex := strings.TrimSpace(os.Getenv("CREDS_KEY"))
	ivHex := strings.TrimSpace(os.Getenv("CREDS_IV"))
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_KEY: %w", err)
	}
	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", fmt.Errorf("CREDS_IV: %w", err)
	}
	if len(iv) != aes.BlockSize {
		return "", fmt.Errorf("CREDS_IV debe ser %d bytes", aes.BlockSize)
	}
	ciphertext, err := hex.DecodeString(strings.TrimSpace(hexCipher))
	if err != nil {
		return "", err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("longitud ciphertext inválida")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	unpadded, err := pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(unpadded), nil
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("pkcs7: longitud inválida")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize {
		return nil, fmt.Errorf("pkcs7: padding inválido")
	}
	for i := 0; i < pad; i++ {
		if data[len(data)-1-i] != byte(pad) {
			return nil, fmt.Errorf("pkcs7: bytes de padding incoherentes")
		}
	}
	return data[:len(data)-pad], nil
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	if pad == 0 {
		pad = blockSize
	}
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}
