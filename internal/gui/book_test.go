package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionBookRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.kvmb")
	items := []connection{{
		Name:     "Test iDRAC",
		Group:    "Lab",
		Target:   "192.0.2.10",
		User:     "root",
		Password: "secret-password",
		Profile:  "dell",
	}}
	if err := saveConnectionBook(path, "master-password", items); err != nil {
		t.Fatalf("saveConnectionBook() error = %v", err)
	}
	book, err := loadConnectionBook(path, "master-password")
	if err != nil {
		t.Fatalf("loadConnectionBook() error = %v", err)
	}
	if len(book.Items) != 1 {
		t.Fatalf("loaded %d items, want 1", len(book.Items))
	}
	if got := book.Items[0].Password; got != "secret-password" {
		t.Fatalf("loaded password = %q, want saved password", got)
	}
}

func TestConnectionBookRejectsWrongPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.kvmb")
	if err := saveConnectionBook(path, "right-password", []connection{{Name: "A"}}); err != nil {
		t.Fatalf("saveConnectionBook() error = %v", err)
	}
	if _, err := loadConnectionBook(path, "wrong-password"); err == nil {
		t.Fatal("loadConnectionBook() with wrong password succeeded")
	}
}

func TestConnectionBookDoesNotStorePlaintextPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.kvmb")
	if err := saveConnectionBook(path, "master-password", []connection{{Password: "plain-kvm-password"}}); err != nil {
		t.Fatalf("saveConnectionBook() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(data), "plain-kvm-password") {
		t.Fatal("encrypted connection book contains plaintext KVM password")
	}
}
