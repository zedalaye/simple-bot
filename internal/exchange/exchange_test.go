package exchange

import (
	"bot/internal/bot"
	"errors"
	"fmt"
	"testing"
	"time"

	ccxt "github.com/ccxt/ccxt/go/v4"
)

func TestIsTransientNetworkError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// Erreurs typées ccxt réseau/throttle : retry légitime.
		{"ccxt NetworkError", &ccxt.Error{Type: ccxt.NetworkErrorErrType, Message: "boom"}, true},
		{"ccxt RequestTimeout", &ccxt.Error{Type: ccxt.RequestTimeoutErrType, Message: "boom"}, true},
		{"ccxt ExchangeNotAvailable", &ccxt.Error{Type: ccxt.ExchangeNotAvailableErrType, Message: "boom"}, true},
		{"ccxt DDoSProtection", &ccxt.Error{Type: ccxt.DDoSProtectionErrType, Message: "boom"}, true},
		{"ccxt RateLimitExceeded", &ccxt.Error{Type: ccxt.RateLimitExceededErrType, Message: "boom"}, true},

		// Erreurs métier : ne pas réessayer.
		{"ccxt InsufficientFunds", &ccxt.Error{Type: ccxt.InsufficientFundsErrType, Message: "no money"}, false},
		{"ccxt InvalidOrder", &ccxt.Error{Type: ccxt.InvalidOrderErrType, Message: "bad order"}, false},
		{"ccxt AuthenticationError", &ccxt.Error{Type: ccxt.AuthenticationErrorErrType, Message: "bad key"}, false},

		// Signatures brutes de la stdlib (erreur non typée ccxt).
		{"raw connection reset", errors.New(`Get "https://eea.okx.com": read: connection reset by peer`), true},
		{"raw context deadline", errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), true},
		{"raw i/o timeout", errors.New("dial tcp: i/o timeout"), true},
		{"raw EOF", errors.New("unexpected EOF"), true},

		// Erreurs quelconques non réseau : ne pas réessayer.
		{"nil", nil, false},
		{"plain business error", errors.New("cycle already closed"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientNetworkError(tc.err); got != tc.want {
				t.Fatalf("isTransientNetworkError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// withFastRetries abaisse le délai de backoff le temps d'un test pour ne pas dormir
// réellement plusieurs secondes.
func withFastRetries(t *testing.T) {
	t.Helper()
	orig := baseRetryDelay
	baseRetryDelay = time.Millisecond
	t.Cleanup(func() { baseRetryDelay = orig })
}

func TestRetryIdempotentReessaieLeReseau(t *testing.T) {
	withFastRetries(t)

	calls := 0
	netErr := &ccxt.Error{Type: ccxt.NetworkErrorErrType, Message: "reset"}
	err := retryIdempotent(func() error {
		calls++
		if calls < 3 {
			return netErr
		}
		return nil
	})
	if err != nil {
		t.Fatalf("attendu succès après retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("attendu 3 tentatives, got %d", calls)
	}
}

func TestRetryWithBackoffNeReessaiePasLeReseau(t *testing.T) {
	withFastRetries(t)

	// Chemin conservateur (placement d'ordre) : une erreur réseau ambiguë ne doit
	// PAS être rejouée, sous peine de dupliquer un ordre déjà exécuté côté exchange.
	calls := 0
	err := retryWithBackoff(func() error {
		calls++
		return &ccxt.Error{Type: ccxt.NetworkErrorErrType, Message: "reset"}
	})
	if err == nil {
		t.Fatal("attendu une erreur")
	}
	if calls != 1 {
		t.Fatalf("attendu 1 seule tentative (pas de retry réseau), got %d", calls)
	}
}

func TestRetryIdempotentNeReessaiePasLeMetier(t *testing.T) {
	withFastRetries(t)

	calls := 0
	err := retryIdempotent(func() error {
		calls++
		return &ccxt.Error{Type: ccxt.InsufficientFundsErrType, Message: "no money"}
	})
	if err == nil {
		t.Fatal("attendu une erreur")
	}
	if calls != 1 {
		t.Fatalf("attendu 1 seule tentative (erreur métier), got %d", calls)
	}
}

func TestDecimalsOf(t *testing.T) {
	cases := []struct {
		step float64
		want int
	}{
		{0.1, 1}, {0.01, 2}, {0.00000001, 8}, {1, 0}, {10, 0}, {0.5, 1},
	}
	for _, c := range cases {
		if got := decimalsOf(c.step); got != c.want {
			t.Errorf("decimalsOf(%g) = %d, attendu %d", c.step, got, c.want)
		}
	}
}

// TestOKXTimestampHeaderIsADate protège le blocage de ccxt en 4.5.78 (cf. go.mod).
// ccxt convertit les en-têtes signés avec fmt.Sprintf("%v", v) avant l'envoi
// (exchange_req.go). À partir de 4.5.79, OK-ACCESS-TIMESTAMP est un *string : la
// conversion produit une adresse mémoire et OKX refuse toute requête authentifiée
// (code 50112). Si ce test échoue après une montée de version de ccxt, ne pas
// déployer. Si ccxt corrige la sérialisation en amont, adapter ce test en conséquence.
func TestOKXTimestampHeaderIsADate(t *testing.T) {
	ex := ccxt.NewMyokx(map[string]interface{}{"apiKey": "k", "secret": "s", "password": "p"})
	// L'API privée se désigne par "private" (≤ 4.5.78) ou []string{"private"}
	// (≥ 4.5.79), et Sign renvoie any ou map[string]any : on essaie les deux formes.
	var headers map[string]any
	for _, api := range []any{"private", []string{"private"}} {
		if h := signedHeaders(ex, api); h != nil {
			headers = h
			break
		}
	}
	if headers == nil {
		t.Fatal("aucun en-tête signé : l'API Sign de ccxt a changé, adapter ce test")
	}
	got := fmt.Sprintf("%v", headers["OK-ACCESS-TIMESTAMP"]) // même conversion que ccxt
	if _, err := time.Parse("2006-01-02T15:04:05.000Z", got); err != nil {
		t.Fatalf("OK-ACCESS-TIMESTAMP sérialisé en %q, pas une date ISO 8601 : OKX refusera l'authentification", got)
	}
}

// signedHeaders signe une requête privée OKX avec la forme d'API donnée ; nil si
// cette version de ccxt ne l'accepte pas (elle panique alors sur le type d'argument).
func signedHeaders(ex *ccxt.Myokx, api any) (headers map[string]any) {
	defer func() {
		if recover() != nil {
			headers = nil
		}
	}()
	req, _ := any(ex.Sign("account/trade-fee", api, "GET", map[string]any{"instType": "SPOT"})).(map[string]any)
	headers, _ = req["headers"].(map[string]any)
	return headers
}

// Un ordre accepté par l'exchange mais pas encore visible en lecture (OKX 51603)
// doit finir par être relu, sans erreur remontée à l'appelant.
func TestConfirmOrderRelitApresOrderNotFound(t *testing.T) {
	calls := 0
	notFound := errors.New(`[OrderNotFound] myokx {"code":"51603","data":[],"msg":"Order does not exist"}`)
	id, amount, price, status := "42", 0.0006, 60000.0, "closed"
	got, err := confirmOrder("42", func() (bot.Order, error) {
		calls++
		if calls < 3 {
			return bot.Order{}, notFound
		}
		return bot.Order{Id: &id, Amount: &amount, Price: &price, Status: &status}, nil
	}, 0.0006, 60000, []time.Duration{0, 0, 0, 0}, func(error) { t.Fatal("abandon inattendu") })
	if err != nil {
		t.Fatalf("aucune erreur attendue, got %v", err)
	}
	if calls != 3 || *got.Status != "closed" {
		t.Fatalf("attendu l'ordre relu au 3e essai, got calls=%d status=%s", calls, *got.Status)
	}
}

// Si l'ordre reste illisible, on renvoie l'ordre tel qu'envoyé (statut open) et
// SURTOUT pas d'erreur : sinon il serait orphelin puis reposé (double vente du 2026-11-22).
func TestConfirmOrderNeRenvoieJamaisDErreur(t *testing.T) {
	gaveUp := false
	got, err := confirmOrder("42", func() (bot.Order, error) {
		return bot.Order{}, errors.New("[OrderNotFound] 51603")
	}, 0.00059433, 60865.9, []time.Duration{0, 0}, func(error) { gaveUp = true })
	if err != nil {
		t.Fatalf("aucune erreur attendue, got %v", err)
	}
	if !gaveUp {
		t.Fatal("onGiveUp aurait dû être appelé")
	}
	if *got.Id != "42" || *got.Amount != 0.00059433 || *got.Price != 60865.9 || *got.Status != "open" {
		t.Fatalf("ordre reconstruit inattendu : id=%s amount=%v price=%v status=%s",
			*got.Id, *got.Amount, *got.Price, *got.Status)
	}
}
