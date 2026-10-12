package fcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A token endpoint that accepts the request and never answers must not block the
// notification worker: the token request gives up like the Java SDK.
func TestTokenRequestTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	creds, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "p", "private_key_id": "k", "private_key": string(pemKey),
		"client_email": "a@p.iam.gserviceaccount.com", "client_id": "1", "token_uri": srv.URL,
	})
	old := tokenTimeout
	tokenTimeout = 200 * time.Millisecond
	defer func() { tokenTimeout = old }()
	s, err := New(context.Background(), string(creds), nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.SendEachForMulticast(context.Background(), Message{Tokens: []string{"t"}, Title: "x"})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a token error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("token request still blocked")
	}
}
