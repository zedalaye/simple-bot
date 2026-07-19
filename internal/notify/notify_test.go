package notify

import (
	"errors"
	"testing"
)

type recorder struct {
	events []Event
	err    error
}

func (r *recorder) Notify(e Event) error {
	r.events = append(r.events, e)
	return r.err
}

// Un canal en échec ne doit pas priver les autres de l'événement : c'est ce qui
// permet de faire tourner Telegram et le relay en parallèle sans que la panne de
// l'un fasse taire l'autre.
func TestMultiNotifyAllChannelsDespiteFailure(t *testing.T) {
	failing := &recorder{err: errors.New("relay indisponible")}
	working := &recorder{}

	err := Multi{failing, working}.Notify(Event{Kind: KindBuyFilled})

	if err == nil {
		t.Fatal("Notify() = nil, une erreur du canal en échec était attendue")
	}
	if len(working.events) != 1 {
		t.Errorf("le canal sain a reçu %d événement(s), want 1", len(working.events))
	}
}

// Un Notifier nil dans la liste (canal non configuré) doit être ignoré sans
// paniquer.
func TestMultiNotifySkipsNil(t *testing.T) {
	working := &recorder{}

	if err := (Multi{nil, working}).Notify(Event{Kind: KindError}); err != nil {
		t.Fatalf("Notify() = %v, want nil", err)
	}
	if len(working.events) != 1 {
		t.Errorf("le canal sain a reçu %d événement(s), want 1", len(working.events))
	}
}

func TestMultiNotifyAllSucceed(t *testing.T) {
	a, b := &recorder{}, &recorder{}

	if err := (Multi{a, b}).Notify(Event{Kind: KindSellFilled}); err != nil {
		t.Fatalf("Notify() = %v, want nil", err)
	}
	if len(a.events) != 1 || len(b.events) != 1 {
		t.Errorf("diffusion incomplète : a=%d b=%d, want 1 et 1", len(a.events), len(b.events))
	}
}
