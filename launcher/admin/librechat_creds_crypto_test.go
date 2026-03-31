package admin

import "testing"

// Comprueba que encrypt/decrypt coinciden con el esquema legacy AES-CBC de LibreChat (CREDS_* hex).
func TestLibreChatKeyEncryptDecryptRoundTrip(t *testing.T) {
	// 32 bytes key, 16 bytes IV (hex como en .env.librechat)
	const keyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	const ivHex = "0102030405060708090a0b0c0d0e0f10"
	t.Setenv("CREDS_KEY", keyHex)
	t.Setenv("CREDS_IV", ivHex)

	plain := "sk-test-roundtrip-key-123456789012"
	enc, err := encryptLibreChatKeyValue(plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decryptLibreChatKeyValue(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("roundtrip: want %q got %q", plain, got)
	}
}
