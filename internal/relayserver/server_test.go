package relayserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"bot/internal/logger"
	"bot/internal/relay/contract"
)

func TestMain(m *testing.M) {
	_ = logger.InitLogger("error", "")
	os.Exit(m.Run())
}

const (
	ingestToken = "ingest-secret"
	apiToken    = "api-secret"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv := New(Config{
		IngestToken: ingestToken,
		APIToken:    apiToken,
		Silence:     5 * time.Minute,
	}, NewStore())

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func do(t *testing.T, ts *httptest.Server, method, path, token string, body any) (*http.Response, map[string]any) {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("sérialisation : %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatalf("requête : %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("appel %s %s : %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp, decoded
}

func sampleSnapshot() contract.Snapshot {
	return contract.Snapshot{
		Instance: "mexc",
		Version:  "1.4.2",
		At:       time.Now().UTC(),
		Exchange: "mexc",
		Pair:     "BTC/USDC",
		Price:    "58234.10",
	}
}

// Les deux surfaces ont des jetons distincts : le jeton du bot ne doit pas
// ouvrir l'API mobile, et réciproquement.
func TestTokensAreNotInterchangeable(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []struct {
		name, method, path, token string
		body                      any
		wantStatus                int
	}{
		{"ingestion sans jeton", "POST", "/ingest/snapshot", "", sampleSnapshot(), http.StatusUnauthorized},
		{"ingestion avec le jeton d'API", "POST", "/ingest/snapshot", apiToken, sampleSnapshot(), http.StatusUnauthorized},
		{"ingestion avec le bon jeton", "POST", "/ingest/snapshot", ingestToken, sampleSnapshot(), http.StatusOK},
		{"API sans jeton", "GET", "/api/state", "", nil, http.StatusUnauthorized},
		{"API avec le jeton d'ingestion", "GET", "/api/state", ingestToken, nil, http.StatusUnauthorized},
		{"API avec le bon jeton", "GET", "/api/state", apiToken, nil, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := do(t, ts, tc.method, tc.path, tc.token, tc.body)
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("statut = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

// Un jeton non configuré doit fermer la surface, pas l'ouvrir.
func TestMissingTokenClosesSurface(t *testing.T) {
	srv := New(Config{IngestToken: "", APIToken: apiToken}, NewStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, _ := do(t, ts, "POST", "/ingest/snapshot", "", sampleSnapshot())
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("statut = %d, want 401 (jeton non configuré)", resp.StatusCode)
	}
}

func TestSnapshotThenState(t *testing.T) {
	_, ts := newTestServer(t)

	snap := sampleSnapshot()
	snap.ActiveCycles = 5
	snap.OpenOrders = 7
	do(t, ts, "POST", "/ingest/snapshot", ingestToken, snap)

	_, body := do(t, ts, "GET", "/api/state", apiToken, nil)
	instances, ok := body["instances"].([]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("instances = %v, want 1", body["instances"])
	}

	view := instances[0].(map[string]any)
	if view["instance"] != "mexc" || view["online"] != true {
		t.Errorf("view = %v, want instance=mexc online=true", view)
	}
	stored := view["snapshot"].(map[string]any)
	if stored["active_cycles"] != float64(5) || stored["open_orders"] != float64(7) {
		t.Errorf("snapshot = %v", stored)
	}
}

func TestCommandRoundTrip(t *testing.T) {
	_, ts := newTestServer(t)

	do(t, ts, "POST", "/ingest/snapshot", ingestToken, sampleSnapshot())

	resp, cmd := do(t, ts, "POST", "/api/commands", apiToken, CommandRequest{
		Instance: "mexc", Action: "pause",
	})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("statut = %d, want 202", resp.StatusCode)
	}
	id, _ := cmd["id"].(string)
	if id == "" {
		t.Fatal("identifiant de commande absent")
	}

	// Le snapshot suivant doit rapporter la commande au bot.
	_, reply := do(t, ts, "POST", "/ingest/snapshot", ingestToken, sampleSnapshot())
	commands, ok := reply["commands"].([]any)
	if !ok || len(commands) != 1 {
		t.Fatalf("commands = %v, want 1", reply["commands"])
	}
	if got := commands[0].(map[string]any); got["id"] != id || got["action"] != "pause" {
		t.Errorf("commande = %v, want id=%s action=pause", got, id)
	}

	// Le bot acquitte : la commande ne doit plus être renvoyée.
	acked := sampleSnapshot()
	acked.Acks = []contract.Ack{{ID: id, OK: true}}
	_, reply = do(t, ts, "POST", "/ingest/snapshot", ingestToken, acked)
	if commands, _ := reply["commands"].([]any); len(commands) != 0 {
		t.Errorf("commands = %v, want vide après acquittement", commands)
	}
}

// Tant qu'elle n'est pas acquittée, la commande est rejouée à chaque snapshot :
// c'est ce qui la rend robuste à la perte d'une réponse HTTP.
func TestCommandRedeliveredUntilAcked(t *testing.T) {
	_, ts := newTestServer(t)
	do(t, ts, "POST", "/ingest/snapshot", ingestToken, sampleSnapshot())
	do(t, ts, "POST", "/api/commands", apiToken, CommandRequest{Instance: "mexc", Action: "resume"})

	for i := range 3 {
		_, reply := do(t, ts, "POST", "/ingest/snapshot", ingestToken, sampleSnapshot())
		if commands, _ := reply["commands"].([]any); len(commands) != 1 {
			t.Fatalf("snapshot %d : commands = %v, want 1", i+1, reply["commands"])
		}
	}
}

// Le relay refuse au plus tôt ce que le bot refuserait de toute façon.
func TestCommandOutsideWhitelistRejected(t *testing.T) {
	_, ts := newTestServer(t)

	for _, action := range []string{"buy", "BuyNow", "", "stop"} {
		resp, body := do(t, ts, "POST", "/api/commands", apiToken, CommandRequest{
			Instance: "mexc", Action: action,
		})
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("action %q : statut = %d, want 400", action, resp.StatusCode)
		}
		if body["error"] == nil {
			t.Errorf("action %q : motif de refus absent", action)
		}
	}
}

func TestEventsStoredAndPaginated(t *testing.T) {
	_, ts := newTestServer(t)

	for _, title := range []string{"Achat rempli", "Vente remplie"} {
		do(t, ts, "POST", "/ingest/event", ingestToken, contract.Event{
			Instance: "mexc", Kind: "buy_filled", Level: contract.LevelInfo, Title: title,
		})
	}

	_, body := do(t, ts, "GET", "/api/events?instance=mexc", apiToken, nil)
	events := body["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("%d événement(s), want 2", len(events))
	}

	// Pagination : ne reprendre que ce qui suit la séquence connue.
	_, body = do(t, ts, "GET", "/api/events?instance=mexc&since=1", apiToken, nil)
	events = body["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("%d événement(s) après since=1, want 1", len(events))
	}
	first := events[0].(map[string]any)
	if first["seq"] != float64(2) {
		t.Errorf("seq = %v, want 2", first["seq"])
	}
}

// Le dead man's switch : le relay produit lui-même l'alerte que le bot ne peut
// pas envoyer, puisqu'elle signale qu'il ne parle plus.
func TestWatchdogReportsSilenceAndRecovery(t *testing.T) {
	srv, ts := newTestServer(t)
	do(t, ts, "POST", "/ingest/snapshot", ingestToken, sampleSnapshot())

	// Rien à signaler tant que les snapshots arrivent.
	srv.checkSilence()
	if events := srv.store.Events("mexc", 0); len(events) != 0 {
		t.Fatalf("%d événement(s) alors que le bot est vivant", len(events))
	}

	// Le temps passe, plus aucun snapshot.
	srv.nowFunc = func() time.Time { return time.Now().Add(10 * time.Minute) }
	srv.checkSilence()

	events := srv.store.Events("mexc", 0)
	if len(events) != 1 {
		t.Fatalf("%d événement(s), want 1 alerte de silence", len(events))
	}
	if events[0].Event.Kind != contract.KindBotSilent || events[0].Event.Level != contract.LevelError {
		t.Errorf("événement = %+v, want bot_silent/error", events[0].Event)
	}

	// L'alerte ne doit pas se répéter à chaque tick.
	srv.checkSilence()
	if events := srv.store.Events("mexc", 0); len(events) != 1 {
		t.Errorf("%d événement(s), l'alerte se répète", len(events))
	}

	// Retour du bot : on le signale une fois.
	srv.nowFunc = time.Now
	do(t, ts, "POST", "/ingest/snapshot", ingestToken, sampleSnapshot())
	srv.checkSilence()

	events = srv.store.Events("mexc", 0)
	if len(events) != 2 {
		t.Fatalf("%d événement(s), want 2 (silence puis retour)", len(events))
	}
	if events[1].Event.Level != contract.LevelInfo {
		t.Errorf("événement de retour = %+v, want niveau info", events[1].Event)
	}
}
