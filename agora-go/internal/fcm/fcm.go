// Package fcm sends Firebase Cloud Messaging notifications like the Firebase
// Admin Java SDK's FirebaseMessaging.sendEachForMulticast (one HTTP v1 request
// per token), authenticated with the FIREBASE_CREDENTIALS_JSON service account.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const scope = "https://www.googleapis.com/auth/firebase.messaging"

// Message is one multicast notification (MulticastMessage).
type Message struct {
	Tokens []string
	Title  string
	Body   string
	Data   map[string]string
}

// BatchResponse mirrors com.google.firebase.messaging.BatchResponse counts.
type BatchResponse struct {
	SuccessCount int
	FailureCount int
}

// Sender sends notifications. A nil credentials JSON gives a sender whose
// calls fail like the Kotlin code without FirebaseApp (IllegalStateException).
type Sender struct {
	projectID string
	ts        oauth2.TokenSource
	client    *http.Client
	endpoint  string
	log       *slog.Logger
}

// ErrNotInitialized mirrors "FirebaseApp with name [DEFAULT] doesn't exist".
var ErrNotInitialized = errors.New("IllegalStateException: FirebaseApp with name [DEFAULT] doesn't exist")

// New builds a sender from the service account JSON (may be empty).
// FCM_ENDPOINT overrides the API base URL (parity harness only).
func New(ctx context.Context, credentialsJSON string, log *slog.Logger) (*Sender, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &Sender{log: log, client: &http.Client{Timeout: 30 * time.Second}, endpoint: "https://fcm.googleapis.com"}
	if v := os.Getenv("FCM_ENDPOINT"); v != "" {
		s.endpoint = v
	}
	if credentialsJSON == "" {
		return s, nil
	}
	creds, err := google.CredentialsFromJSON(ctx, []byte(credentialsJSON), scope)
	if err != nil {
		return nil, fmt.Errorf("FIREBASE_CREDENTIALS_JSON: %w", err)
	}
	s.projectID = creds.ProjectID
	s.ts = creds.TokenSource
	return s, nil
}

// SendEachForMulticast sends one request per token, concurrently (the Java
// SDK uses a thread pool), and returns the success/failure counts.
func (s *Sender) SendEachForMulticast(ctx context.Context, m Message) (BatchResponse, error) {
	if s.ts == nil {
		return BatchResponse{}, ErrNotInitialized
	}
	if len(m.Tokens) == 0 || len(m.Tokens) > 500 {
		return BatchResponse{}, fmt.Errorf("IllegalArgumentException: tokens list must not be empty or contain more than 500 tokens")
	}
	tok, err := s.ts.Token()
	if err != nil {
		return BatchResponse{}, err
	}
	var mu sync.Mutex
	var br BatchResponse
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, t := range m.Tokens {
		wg.Add(1)
		sem <- struct{}{}
		go func(token string) {
			defer wg.Done()
			defer func() { <-sem }()
			err := s.sendOne(ctx, tok, token, m)
			mu.Lock()
			if err != nil {
				br.FailureCount++
			} else {
				br.SuccessCount++
			}
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return br, nil
}

func (s *Sender) sendOne(ctx context.Context, tok *oauth2.Token, token string, m Message) error {
	msg := map[string]any{
		"token":        token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
	}
	if len(m.Data) > 0 {
		msg["data"] = m.Data
	}
	body, _ := json.Marshal(map[string]any{"message": msg, "validate_only": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/projects/%s/messages:send", s.endpoint, s.projectID), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	tok.SetAuthHeader(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fcm %d: %s", resp.StatusCode, b)
	}
	return nil
}
