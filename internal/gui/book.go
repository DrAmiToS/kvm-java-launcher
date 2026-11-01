package gui

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	bookVersion       = 1
	bookKDF           = "pbkdf2-sha256"
	bookCipher        = "aes-256-gcm"
	bookKeyBytes      = 32
	bookSaltBytes     = 16
	bookNonceBytes    = 12
	bookKDFIterations = 310000
)

type connectionBook struct {
	Version     int          `json:"version"`
	Items       []connection `json:"items"`
	UnlockModes []string     `json:"unlock_modes,omitempty"`
}

type encryptedBookFile struct {
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	KDFIter    int    `json:"kdf_iter"`
	Cipher     string `json:"cipher"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func defaultBookPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "KVM Java Launcher", "connections.kvmb"), nil
}

func bookExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func loadConnectionBook(path, password string) (*connectionBook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope encryptedBookFile
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("connection book is not valid JSON: %w", err)
	}
	if envelope.Version != bookVersion {
		return nil, fmt.Errorf("unsupported connection book version %d", envelope.Version)
	}
	if envelope.KDF != bookKDF {
		return nil, fmt.Errorf("unsupported connection book KDF %q", envelope.KDF)
	}
	if envelope.Cipher != bookCipher {
		return nil, fmt.Errorf("unsupported connection book cipher %q", envelope.Cipher)
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return nil, fmt.Errorf("connection book salt is invalid: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("connection book nonce is invalid: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("connection book ciphertext is invalid: %w", err)
	}
	iterations := envelope.KDFIter
	if iterations <= 0 {
		iterations = bookKDFIterations
	}
	key, err := deriveBookKey(password, salt, iterations)
	if err != nil {
		return nil, err
	}
	plain, err := decryptBookPayload(key, nonce, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("could not decrypt connection book: wrong password or damaged file")
	}
	var book connectionBook
	if err := json.Unmarshal(plain, &book); err != nil {
		return nil, fmt.Errorf("connection book payload is invalid: %w", err)
	}
	if book.Version == 0 {
		book.Version = bookVersion
	}
	if len(book.UnlockModes) == 0 {
		book.UnlockModes = []string{"password"}
	}
	return &book, nil
}

func saveConnectionBook(path, password string, items []connection) error {
	book := connectionBook{
		Version:     bookVersion,
		Items:       items,
		UnlockModes: []string{"password"},
	}
	plain, err := json.MarshalIndent(book, "", "  ")
	if err != nil {
		return err
	}
	salt := make([]byte, bookSaltBytes)
	nonce := make([]byte, bookNonceBytes)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	key, err := deriveBookKey(password, salt, bookKDFIterations)
	if err != nil {
		return err
	}
	ciphertext, err := encryptBookPayload(key, nonce, plain)
	if err != nil {
		return err
	}
	envelope := encryptedBookFile{
		Version:    bookVersion,
		KDF:        bookKDF,
		KDFIter:    bookKDFIterations,
		Cipher:     bookCipher,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func deriveBookKey(password string, salt []byte, iterations int) ([]byte, error) {
	if iterations <= 0 {
		return nil, fmt.Errorf("invalid KDF iteration count %d", iterations)
	}
	return pbkdf2.Key(sha256.New, password, salt, iterations, bookKeyBytes)
}

func encryptBookPayload(key, nonce, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, nonce, plain, nil), nil
}

func decryptBookPayload(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}
