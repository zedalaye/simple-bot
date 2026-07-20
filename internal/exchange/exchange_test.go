package exchange

import (
	"errors"
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
		{"raw connection reset", errors.New(`Get "https://api.mexc.com": read: connection reset by peer`), true},
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
