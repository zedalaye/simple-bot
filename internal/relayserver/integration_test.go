package relayserver

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"bot/internal/dashboard"
	"bot/internal/relay"
)

// Ce test fait dialoguer les deux moitiés réelles — le client embarqué dans le
// bot (internal/relay) et le serveur du relay — sur du vrai HTTP.
//
// C'est ce qui justifie le contrat partagé : si les deux côtés divergeaient sur
// un nom de champ, tout le reste compilerait et passerait, mais ce test
// échouerait.

// fakeBot est un bot minimal : il n'expose que ce dont le relay a besoin.
type fakeBot struct {
	mu     sync.Mutex
	paused bool
}

func (f *fakeBot) Status() (dashboard.StatusSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return dashboard.StatusSnapshot{
		Version:      "1.4.2",
		Exchange:     "mexc",
		Pair:         "BTC/USDC",
		Quote:        "USDC",
		Price:        "58234.10",
		ActiveCycles: 5,
		OpenOrders:   7,
		Paused:       f.paused,
		Uptime:       90 * time.Minute,
		LastCheckAgo: 42 * time.Second,
	}, nil
}

func (f *fakeBot) Cycles() ([]dashboard.CycleView, error) { return nil, nil }
func (f *fakeBot) PnL() (dashboard.PnLSnapshot, error)    { return dashboard.PnLSnapshot{}, nil }

func (f *fakeBot) Balance() (dashboard.BalanceSnapshot, error) {
	return dashboard.BalanceSnapshot{
		Exchange: "mexc", Quote: "USDC", Total: "1240.55", Locked: "310.20",
	}, nil
}

func (f *fakeBot) Pause() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = true
	return nil
}

func (f *fakeBot) Resume() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = false
	return nil
}

func (f *fakeBot) isPaused() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paused
}

// waitFor attend qu'une condition devienne vraie, ou échoue.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("délai dépassé en attendant : %s", what)
}

func TestBotAndRelaySpeakTheSameContract(t *testing.T) {
	srv := New(Config{
		IngestToken: ingestToken,
		APIToken:    apiToken,
		Silence:     time.Minute,
	}, NewStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	bot := &fakeBot{}
	client := relay.New(relay.Config{
		URL:      ts.URL,
		Token:    ingestToken,
		Instance: "mexc",
		// Cadence resserrée : le test ne doit pas attendre une minute.
		Interval:        20 * time.Millisecond,
		BalanceInterval: time.Hour,
	}, bot)

	client.Start(t.Context())

	// 1. Le bot pousse son état, le relay le sert à l'application.
	waitFor(t, "la remontée du premier snapshot", func() bool {
		_, body := do(t, ts, "GET", "/api/state", apiToken, nil)
		instances, _ := body["instances"].([]any)
		return len(instances) == 1
	})

	_, body := do(t, ts, "GET", "/api/state", apiToken, nil)
	view := body["instances"].([]any)[0].(map[string]any)
	snap := view["snapshot"].(map[string]any)

	if view["online"] != true {
		t.Errorf("online = %v, want true", view["online"])
	}
	// Les champs traversent bien les deux moitiés, sans perte ni renommage.
	if snap["price"] != "58234.10" || snap["open_orders"] != float64(7) ||
		snap["uptime_s"] != float64(5400) || snap["version"] != "1.4.2" {
		t.Errorf("snapshot mal transmis : %v", snap)
	}

	// 2. L'application demande une pause.
	resp, cmd := do(t, ts, "POST", "/api/commands", apiToken, CommandRequest{
		Instance: "mexc", Action: "pause",
	})
	if resp.StatusCode != 202 {
		t.Fatalf("mise en file refusée : statut %d", resp.StatusCode)
	}
	id := cmd["id"].(string)

	// 3. Le bot la reçoit dans la réponse à son snapshot suivant et l'exécute,
	//    sans qu'aucune connexion entrante n'ait été ouverte vers lui.
	waitFor(t, "l'exécution de la pause par le bot", bot.isPaused)

	// 4. L'acquittement remonte, et l'application peut le constater.
	waitFor(t, "la remontée de l'acquittement", func() bool {
		_, body := do(t, ts, "GET", "/api/state", apiToken, nil)
		view := body["instances"].([]any)[0].(map[string]any)
		commands, _ := view["commands"].([]any)
		for _, raw := range commands {
			c := raw.(map[string]any)
			if c["id"] == id && c["ok"] == true && c["acked_at"] != nil {
				return true
			}
		}
		return false
	})

	// 5. L'état poussé ensuite reflète la pause : la boucle est bouclée.
	waitFor(t, "la propagation de l'état en pause", func() bool {
		_, body := do(t, ts, "GET", "/api/state", apiToken, nil)
		view := body["instances"].([]any)[0].(map[string]any)
		return view["snapshot"].(map[string]any)["paused"] == true
	})
}
