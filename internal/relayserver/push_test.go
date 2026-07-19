package relayserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Une paire VAPID de test — générée pour ce test, sans usage réel.
const (
	testVAPIDPublic  = "BCpviXTBo1T2sjvmW_Z0udWRKdToQaHsypUJv6OQkY0r4CMDiLBJjzdfLf4cd2kCafjl8fMKl4MvQy2inqkyzIw"
	testVAPIDPrivate = "czFpyWlR1b_gLRTpTGmzXT0vGKnpzywssxN5o48ixYM"
)

func newPushServer(t *testing.T) (*Store, *httptest.Server) {
	t.Helper()
	store := NewStore()
	srv := New(Config{
		IngestToken: ingestToken,
		APIToken:    apiToken,
		Push: PushConfig{
			PublicKey:  testVAPIDPublic,
			PrivateKey: testVAPIDPrivate,
			Subscriber: "mailto:test@example.com",
		},
	}, store)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return store, ts
}

// La clé publique doit être servie : le navigateur en a besoin pour s'abonner,
// et elle n'est pas secrète.
func TestPushKeyServed(t *testing.T) {
	_, ts := newPushServer(t)

	resp, body := do(t, ts, "GET", "/api/push/key", apiToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d, want 200", resp.StatusCode)
	}
	if body["key"] != testVAPIDPublic {
		t.Errorf("key = %v, want la clé publique VAPID", body["key"])
	}
}

// Sans paire VAPID, l'application doit l'apprendre explicitement plutôt que de
// tenter un abonnement voué à l'échec.
func TestPushKeyUnavailableWhenUnconfigured(t *testing.T) {
	srv := New(Config{APIToken: apiToken}, NewStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, _ := do(t, ts, "GET", "/api/push/key", apiToken, nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("statut = %d, want 503", resp.StatusCode)
	}
}

func TestPushSubscribeAndUnsubscribe(t *testing.T) {
	store, ts := newPushServer(t)

	sub := map[string]any{
		"endpoint": "https://fcm.googleapis.com/fcm/send/abc123",
		"keys":     map[string]string{"auth": "YXV0aA", "p256dh": "cDI1NmRo"},
	}

	resp, _ := do(t, ts, "POST", "/api/push/subscribe", apiToken, sub)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("statut = %d, want 201", resp.StatusCode)
	}
	if got := store.Subscriptions(); len(got) != 1 || got[0].Keys.Auth != "YXV0aA" {
		t.Fatalf("abonnements = %+v, want 1 avec ses clés", got)
	}

	// Se réabonner depuis le même navigateur remplace, sans dupliquer.
	do(t, ts, "POST", "/api/push/subscribe", apiToken, sub)
	if got := store.Subscriptions(); len(got) != 1 {
		t.Errorf("%d abonnements après réabonnement, want 1", len(got))
	}

	resp, _ = do(t, ts, "POST", "/api/push/unsubscribe", apiToken, map[string]string{
		"endpoint": "https://fcm.googleapis.com/fcm/send/abc123",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d, want 200", resp.StatusCode)
	}
	if got := store.Subscriptions(); len(got) != 0 {
		t.Errorf("%d abonnements après désinscription, want 0", len(got))
	}
}

// Un abonnement sans clés serait inutilisable : le chiffrement en dépend.
func TestPushSubscribeRejectsIncomplete(t *testing.T) {
	store, ts := newPushServer(t)

	cases := []map[string]any{
		{"endpoint": "https://example.com/x"},
		{"keys": map[string]string{"auth": "a", "p256dh": "b"}},
		{"endpoint": "https://example.com/x", "keys": map[string]string{"auth": "a"}},
	}

	for _, body := range cases {
		resp, _ := do(t, ts, "POST", "/api/push/subscribe", apiToken, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%v : statut = %d, want 400", body, resp.StatusCode)
		}
	}
	if got := store.Subscriptions(); len(got) != 0 {
		t.Errorf("%d abonnement(s) enregistré(s) malgré le refus", len(got))
	}
}

// Le push est optionnel : sans abonnement ni configuration, la diffusion ne doit
// ni paniquer ni bloquer l'ingestion.
func TestBroadcastIsNoopWithoutSubscribers(t *testing.T) {
	_, ts := newPushServer(t)

	resp, _ := do(t, ts, "POST", "/ingest/event", ingestToken, map[string]any{
		"instance": "mexc", "kind": "error", "level": "error", "title": "test",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("statut = %d, want 202", resp.StatusCode)
	}
}
