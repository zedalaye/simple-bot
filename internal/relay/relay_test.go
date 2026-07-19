package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"bot/internal/dashboard"
	"bot/internal/logger"
	"bot/internal/notify"
)

// TestMain initialise le logger (le relay journalise ses échecs) au niveau error
// pour éviter tout nil deref pendant les tests.
func TestMain(m *testing.M) {
	_ = logger.InitLogger("error", "")
	os.Exit(m.Run())
}

// fakeSource simule le bot : elle compte les appels de contrôle pour vérifier
// qu'une commande a bien été exécutée — ou justement pas.
type fakeSource struct {
	mu      sync.Mutex
	pauses  int
	resumes int

	status     dashboard.StatusSnapshot
	balance    dashboard.BalanceSnapshot
	balanceErr error
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		status: dashboard.StatusSnapshot{
			Version:      "1.4.2",
			Exchange:     "mexc",
			Pair:         "BTC/USDC",
			Quote:        "USDC",
			Price:        "58234.10",
			RSI:          "38",
			RSITimeframe: "4h",
			ActiveCycles: 5,
			OpenCycles:   2,
			OpenOrders:   7,
			Uptime:       90 * time.Minute,
			LastCheckAgo: 42 * time.Second,
		},
		balance: dashboard.BalanceSnapshot{
			Exchange: "mexc",
			Quote:    "USDC",
			Total:    "1240.55",
			Locked:   "310.20",
			Lines: []dashboard.BalanceLine{
				{Asset: "BTC", Amount: "0.01", Locked: "0.004", Value: "≈ 582.34 USDC"},
			},
		},
	}
}

func (f *fakeSource) Status() (dashboard.StatusSnapshot, error) { return f.status, nil }
func (f *fakeSource) Cycles() ([]dashboard.CycleView, error)    { return nil, nil }
func (f *fakeSource) PnL() (dashboard.PnLSnapshot, error)       { return dashboard.PnLSnapshot{}, nil }

func (f *fakeSource) Balance() (dashboard.BalanceSnapshot, error) {
	return f.balance, f.balanceErr
}

func (f *fakeSource) Pause() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauses++
	return nil
}

func (f *fakeSource) Resume() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes++
	return nil
}

func (f *fakeSource) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pauses, f.resumes
}

// testServer capture les corps reçus et sert des commandes scriptées.
type testServer struct {
	*httptest.Server

	mu        sync.Mutex
	snapshots []map[string]any
	events    []map[string]any
	authSeen  []string
	// reply est consulté à chaque snapshot pour décider des commandes à renvoyer.
	reply func(callNum int) snapshotReply
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{}

	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("corps illisible sur %s : %v", r.URL.Path, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		ts.mu.Lock()
		ts.authSeen = append(ts.authSeen, r.Header.Get("Authorization"))
		var n int
		switch r.URL.Path {
		case "/ingest/snapshot":
			ts.snapshots = append(ts.snapshots, body)
			n = len(ts.snapshots)
		case "/ingest/event":
			ts.events = append(ts.events, body)
		}
		reply := ts.reply
		ts.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/ingest/snapshot" && reply != nil {
			_ = json.NewEncoder(w).Encode(reply(n))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))

	t.Cleanup(ts.Close)
	return ts
}

func (ts *testServer) lastSnapshot(t *testing.T) map[string]any {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.snapshots) == 0 {
		t.Fatal("aucun snapshot reçu")
	}
	return ts.snapshots[len(ts.snapshots)-1]
}

func newTestClient(ts *testServer, src dashboard.Source) *Client {
	c := New(Config{
		URL:             ts.URL,
		Token:           "secret",
		Instance:        "mexc",
		Interval:        time.Minute,
		BalanceInterval: 15 * time.Minute,
	}, src)
	c.retryBackoff = 0
	return c
}

func TestSnapshotCarriesState(t *testing.T) {
	ts := newTestServer(t)
	src := newFakeSource()
	c := newTestClient(ts, src)

	if ok := c.sendSnapshot(true); !ok {
		t.Fatal("sendSnapshot() = false, want true")
	}

	snap := ts.lastSnapshot(t)

	// Les champs que l'application mobile affiche.
	checks := map[string]any{
		"instance":         "mexc",
		"version":          "1.4.2",
		"pair":             "BTC/USDC",
		"price":            "58234.10",
		"active_cycles":    float64(5),
		"open_cycles":      float64(2),
		"open_orders":      float64(7),
		"uptime_s":         float64(5400),
		"last_check_ago_s": float64(42),
		"paused":           false,
	}
	for key, want := range checks {
		if got := snap[key]; got != want {
			t.Errorf("snapshot[%q] = %v (%T), want %v", key, got, got, want)
		}
	}

	portfolio, ok := snap["portfolio"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot[\"portfolio\"] absent ou mal typé : %v", snap["portfolio"])
	}
	if portfolio["total"] != "1240.55" || portfolio["locked"] != "310.20" {
		t.Errorf("portfolio = %v, want total 1240.55 / locked 310.20", portfolio)
	}

	// Le jeton doit accompagner chaque requête.
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.authSeen[0] != "Bearer secret" {
		t.Errorf("Authorization = %q, want %q", ts.authSeen[0], "Bearer secret")
	}
}

// Sans tick lent, le snapshot ne doit pas embarquer de portefeuille : c'est ce
// qui évite d'interroger l'exchange à chaque minute.
func TestSnapshotOmitsPortfolioOnFastTick(t *testing.T) {
	ts := newTestServer(t)
	c := newTestClient(ts, newFakeSource())

	c.sendSnapshot(false)

	if _, present := ts.lastSnapshot(t)["portfolio"]; present {
		t.Error("le snapshot rapide embarque un portefeuille, want absent")
	}
}

// Une erreur de valorisation ne doit pas faire perdre le reste du snapshot.
func TestSnapshotSurvivesBalanceFailure(t *testing.T) {
	ts := newTestServer(t)
	src := newFakeSource()
	src.balanceErr = http.ErrServerClosed
	c := newTestClient(ts, src)

	if ok := c.sendSnapshot(true); !ok {
		t.Fatal("sendSnapshot() = false, want true malgré l'échec de valorisation")
	}
	snap := ts.lastSnapshot(t)
	if _, present := snap["portfolio"]; present {
		t.Error("portefeuille présent alors que la valorisation a échoué")
	}
	if snap["price"] != "58234.10" {
		t.Error("le reste du snapshot a été perdu")
	}
}

func TestCommandPauseExecutedAndAcked(t *testing.T) {
	ts := newTestServer(t)
	src := newFakeSource()
	c := newTestClient(ts, src)

	ts.reply = func(n int) snapshotReply {
		if n == 1 {
			return snapshotReply{Commands: []command{{ID: "c_1", Action: "pause"}}}
		}
		return snapshotReply{}
	}

	c.sendSnapshot(false) // reçoit et exécute la commande
	c.sendSnapshot(false) // transporte l'acquittement

	if pauses, _ := src.counts(); pauses != 1 {
		t.Errorf("Pause() appelé %d fois, want 1", pauses)
	}

	acks, ok := ts.lastSnapshot(t)["acks"].([]any)
	if !ok || len(acks) != 1 {
		t.Fatalf("acks = %v, want 1 acquittement", ts.lastSnapshot(t)["acks"])
	}
	a := acks[0].(map[string]any)
	if a["id"] != "c_1" || a["ok"] != true {
		t.Errorf("ack = %v, want id=c_1 ok=true", a)
	}
}

// Le cœur de la garantie de sécurité : le relay a beau demander un achat, le bot
// n'a aucun chemin pour l'exécuter.
func TestCommandOutsideWhitelistRefused(t *testing.T) {
	ts := newTestServer(t)
	src := newFakeSource()
	c := newTestClient(ts, src)

	ts.reply = func(n int) snapshotReply {
		if n == 1 {
			return snapshotReply{Commands: []command{
				{ID: "c_evil", Action: "buy"},
				{ID: "c_evil2", Action: "BuyNow"},
			}}
		}
		return snapshotReply{}
	}

	c.sendSnapshot(false)
	c.sendSnapshot(false)

	if pauses, resumes := src.counts(); pauses != 0 || resumes != 0 {
		t.Errorf("une commande hors whitelist a déclenché du contrôle (pause=%d resume=%d)", pauses, resumes)
	}

	acks := ts.lastSnapshot(t)["acks"].([]any)
	if len(acks) != 2 {
		t.Fatalf("acks = %v, want 2", acks)
	}
	for _, raw := range acks {
		a := raw.(map[string]any)
		if a["ok"] != false {
			t.Errorf("ack %v : ok = true, want false (commande refusée)", a)
		}
		if a["error"] == nil {
			t.Errorf("ack %v : motif de refus absent", a)
		}
	}
}

// Un acquittement perdu fait renvoyer la commande : elle doit être réacquittée
// sans être rejouée.
func TestCommandIsIdempotent(t *testing.T) {
	ts := newTestServer(t)
	src := newFakeSource()
	c := newTestClient(ts, src)

	ts.reply = func(n int) snapshotReply {
		if n <= 2 {
			return snapshotReply{Commands: []command{{ID: "c_1", Action: "pause"}}}
		}
		return snapshotReply{}
	}

	c.sendSnapshot(false)
	c.sendSnapshot(false)
	c.sendSnapshot(false)

	if pauses, _ := src.counts(); pauses != 1 {
		t.Errorf("Pause() appelé %d fois, want 1 (commande rejouée)", pauses)
	}
}

func TestNotifySendsEvent(t *testing.T) {
	ts := newTestServer(t)
	c := newTestClient(ts, newFakeSource())

	err := c.Notify(notify.Event{
		Kind:   notify.KindSellFilled,
		Level:  notify.LevelInfo,
		Title:  "Vente remplie — cycle #42",
		Text:   "Profit : 12.40 USDC (+2.1%)",
		Fields: map[string]string{"cycle_id": "42"},
	})
	if err != nil {
		t.Fatalf("Notify() = %v, want nil", err)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.events) != 1 {
		t.Fatalf("%d événement(s) reçu(s), want 1", len(ts.events))
	}
	e := ts.events[0]
	if e["kind"] != "sell_filled" || e["level"] != "info" || e["instance"] != "mexc" {
		t.Errorf("événement = %v", e)
	}
	if fields, ok := e["fields"].(map[string]any); !ok || fields["cycle_id"] != "42" {
		t.Errorf("fields = %v, want cycle_id=42", e["fields"])
	}
}

// Un relay en panne ne doit pas faire échouer silencieusement : Notify remonte
// l'erreur (que le bot logue) après avoir réessayé.
func TestNotifyReturnsErrorAfterRetries(t *testing.T) {
	var calls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, Instance: "mexc"}, newFakeSource())
	c.retryBackoff = 0

	if err := c.Notify(notify.Event{Kind: notify.KindError}); err == nil {
		t.Fatal("Notify() = nil, want une erreur")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != eventAttempts {
		t.Errorf("%d tentative(s), want %d", calls, eventAttempts)
	}
}

// Si le snapshot n'est pas transmis, les acquittements ne doivent pas être
// perdus : ils repartent au tick suivant.
func TestAcksSurviveFailedSnapshot(t *testing.T) {
	var mu sync.Mutex
	var fail bool
	var received []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		mu.Lock()
		received = append(received, body)
		shouldFail := fail
		mu.Unlock()

		if shouldFail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if len(received) == 1 {
			_, _ = w.Write([]byte(`{"commands":[{"id":"c_1","action":"resume"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	src := newFakeSource()
	c := New(Config{URL: srv.URL, Instance: "mexc"}, src)
	c.retryBackoff = 0

	c.sendSnapshot(false) // exécute resume, met l'ack en file

	mu.Lock()
	fail = true
	mu.Unlock()
	if ok := c.sendSnapshot(false); ok {
		t.Fatal("sendSnapshot() = true alors que le relay a répondu 502")
	}

	mu.Lock()
	fail = false
	mu.Unlock()
	c.sendSnapshot(false)

	mu.Lock()
	defer mu.Unlock()
	last := received[len(received)-1]
	acks, ok := last["acks"].([]any)
	if !ok || len(acks) != 1 {
		t.Fatalf("acks = %v, want l'acquittement rejoué après l'échec", last["acks"])
	}
	if acks[0].(map[string]any)["id"] != "c_1" {
		t.Errorf("ack = %v, want id=c_1", acks[0])
	}
}
