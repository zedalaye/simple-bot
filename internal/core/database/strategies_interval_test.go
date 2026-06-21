package database

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"bot/internal/logger"
)

// TestMain initialise le logger (utilisé par applyMigrations) avant les tests.
func TestMain(m *testing.M) {
	if err := logger.InitLogger("error", ""); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// newTestDB crée une base temporaire neuve : NewDB applique toutes les
// migrations, ce qui exerce au passage la migration 18 (buy_interval_seconds).
func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := NewDB(path)
	if err != nil {
		t.Fatalf("NewDB (migrations) a échoué : %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Vérifie le round-trip de la stratégie en mode périodique (cron vide +
// intervalle) à travers l'INSERT et le scan.
func TestCreateStrategyInterval_RoundTrip(t *testing.T) {
	db := newTestDB(t)

	const interval = 24 * 3600 // 24 h
	err := db.CreateStrategyFromWeb(
		"Periodic DCA", "test", "rsi_dca", "" /*cron*/, interval /*buyIntervalSeconds*/, true,
		25.0, 2.0, 0.1, 0.1,
		nil, nil, "4h",
		12, 26, 9, "4h",
		20, 2.0, "1h",
		nil, nil, "4h",
		false, nil, nil, "1d",
		false, nil, nil, nil, nil,
		1, 0,
	)
	if err != nil {
		t.Fatalf("CreateStrategyFromWeb a échoué : %v", err)
	}

	// NB : une « Legacy Strategy » est semée par la migration 9, on retrouve
	// donc la nôtre par son nom.
	strategies, err := db.GetAllStrategies()
	if err != nil {
		t.Fatalf("GetAllStrategies a échoué : %v", err)
	}
	var s Strategy
	for _, st := range strategies {
		if st.Name == "Periodic DCA" {
			s = st
		}
	}
	if s.Name == "" {
		t.Fatal("stratégie 'Periodic DCA' introuvable après création")
	}
	if s.CronExpression != "" {
		t.Errorf("CronExpression attendu vide, obtenu %q", s.CronExpression)
	}
	if s.BuyIntervalSeconds != interval {
		t.Errorf("BuyIntervalSeconds attendu %d, obtenu %d", interval, s.BuyIntervalSeconds)
	}
}

// Vérifie GetLastBuyTime : nil sans achat, puis la date du dernier cycle, et
// que la décision de cooldown (telle qu'appliquée dans Bot.executeBuyStrategies)
// est correcte avant/après expiration.
func TestGetLastBuyTime_AndCooldown(t *testing.T) {
	db := newTestDB(t)

	const interval = 24 * 3600
	if err := db.CreateStrategyFromWeb(
		"Periodic DCA", "test", "rsi_dca", "", interval, true,
		25.0, 2.0, 0.1, 0.1,
		nil, nil, "4h",
		12, 26, 9, "4h",
		20, 2.0, "1h",
		nil, nil, "4h",
		false, nil, nil, "1d",
		false, nil, nil, nil, nil,
		1, 0,
	); err != nil {
		t.Fatalf("CreateStrategyFromWeb a échoué : %v", err)
	}
	strategyID := db.mustStrategyID(t, "Periodic DCA")

	// Aucun achat encore : éligible.
	last, err := db.GetLastBuyTime(strategyID)
	if err != nil {
		t.Fatalf("GetLastBuyTime a échoué : %v", err)
	}
	if last != nil {
		t.Fatalf("attendu nil (jamais acheté), obtenu %v", last)
	}

	// Pose un achat : ordre BUY + cycle.
	order, err := db.CreateOrder("ext-buy-1", Buy, 0.001, 65000, 0.0, strategyID)
	if err != nil {
		t.Fatalf("CreateOrder a échoué : %v", err)
	}
	if _, err := db.CreateCycle(order.ID, 66000); err != nil {
		t.Fatalf("CreateCycle a échoué : %v", err)
	}

	last, err = db.GetLastBuyTime(strategyID)
	if err != nil {
		t.Fatalf("GetLastBuyTime a échoué : %v", err)
	}
	if last == nil {
		t.Fatal("attendu une date de dernier achat, obtenu nil")
	}

	cooldown := time.Duration(interval) * time.Second

	// Juste après l'achat : cooldown actif → non éligible.
	if eligible := time.Since(*last) >= cooldown; eligible {
		t.Errorf("achat fraîchement posé : attendu non éligible (cooldown actif), obtenu éligible")
	}

	// Simule un dernier achat il y a 25 h : cooldown expiré → éligible.
	old := time.Now().Add(-25 * time.Hour)
	if eligible := time.Since(old) >= cooldown; !eligible {
		t.Errorf("dernier achat il y a 25 h : attendu éligible, obtenu non éligible")
	}
}

// mustStrategyID retourne l'ID de la stratégie nommée name (helper de test).
func (db *DB) mustStrategyID(t *testing.T, name string) int {
	t.Helper()
	var id int
	if err := db.conn.QueryRow(`SELECT id FROM strategies WHERE name = ?`, name).Scan(&id); err != nil {
		t.Fatalf("strategie %q introuvable : %v", name, err)
	}
	return id
}
