package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"bot/internal/core/database"
	"bot/internal/exchange"

	"github.com/anthropics/anthropic-sdk-go"
)

// model : Opus 4.8, le modèle le plus capable pour le raisonnement analytique.
const model = anthropic.ModelClaudeOpus4_8

// maxTokens borne la sortie par tour du modèle. Confortable pour du texte
// d'analyse ; le streaming évite tout timeout HTTP.
const maxTokens = 8000

// maxTurns borne le nombre d'aller-retours avec le modèle sur une même requête
// utilisateur, pour éviter toute boucle d'outils runaway.
const maxTurns = 12

// Agent encapsule tout ce dont un échange a besoin : le client Claude, la base
// de l'instance, la paire tradée et l'exchange (pour rafraîchir les bougies).
type Agent struct {
	client       anthropic.Client
	db           *database.DB
	pair         string
	exchangeName string

	mu sync.Mutex         // protège la construction paresseuse de l'exchange
	ex *exchange.Exchange // client exchange, construit à la première synchro
}

// Available indique si l'agent peut fonctionner (clé API présente). La WebUI
// s'en sert pour afficher la page chat ou un message d'aide à la configuration.
func Available() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != ""
}

// NewAgent construit un agent. anthropic.NewClient() lit ANTHROPIC_API_KEY dans
// l'environnement (chargé depuis le .env de l'instance au démarrage).
func NewAgent(db *database.DB, exchangeName, pair string) *Agent {
	return &Agent{client: anthropic.NewClient(), db: db, pair: pair, exchangeName: exchangeName}
}

// getExchange construit le client exchange à la première demande (NewExchange
// fait un LoadMarkets réseau) puis le met en cache. STRICTEMENT lecture seule :
// seul FetchCandles est jamais appelé dessus par l'outil sync_candles ; aucun
// passage d'ordre n'est possible via l'agent.
func (a *Agent) getExchange() (*exchange.Exchange, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ex != nil {
		return a.ex, nil
	}
	ex := exchange.NewExchange(a.exchangeName)
	if ex == nil || ex.IExchange == nil {
		return nil, fmt.Errorf("exchange %q non disponible pour la synchro de bougies", a.exchangeName)
	}
	a.ex = ex
	return a.ex, nil
}

// systemPrompt cadre le rôle de l'agent. On y injecte la paire de l'instance
// pour que le modèle raisonne dans le bon contexte.
func (a *Agent) systemPrompt() string {
	return fmt.Sprintf(`Tu es un assistant d'analyse quantitative intégré à un bot de trading crypto (paire %s).
Ton rôle : aider à comprendre et à améliorer les stratégies de trading de l'utilisateur.

Tu disposes d'outils pour interroger la base réelle du bot, rafraîchir les bougies
depuis l'exchange et lancer des backtests sur le vrai moteur de simulation. Règles :
- Ne propose JAMAIS un changement de paramètre sans l'avoir d'abord backtesté. Compare
  la variante à la configuration de base avec run_backtest et cite les chiffres.
- Commence par découvrir les stratégies (list_strategies) avant de raisonner.
- Les bougies en base peuvent DATER si le bot est éteint. Utilise sync_candles pour
  rafraîchir une timeframe depuis l'exchange (lecture seule) quand la fraîcheur compte :
  question sur les conditions récentes ou le prix actuel, ou pour inclure les toutes
  dernières bougies dans un backtest. Inutile de rafraîchir pour une analyse purement
  historique — n'appelle pas cet outil sans raison (il sollicite l'exchange).
- Un backtest se juge sur plusieurs métriques à la fois : PnL réalisé, PnL latent
  (stock non vendu), cycles/jour, capital de pic (capital mobilisé), win rate.
  Méfie-toi d'un PnL réalisé flatteur obtenu en accumulant du stock latent.
- Pour lire les conditions actuelles (RSI, volatilité, tendance), utilise
  get_market_snapshot plutôt que de deviner. Pour comparer plusieurs réglages,
  utilise sweep_backtest (une grille en un appel) plutôt que run_backtest répété.
- Sois concret et honnête : si une piste dégrade les résultats, dis-le.
- Réponds en français, de façon concise. Termine par une recommandation claire.

Tu n'as aucun moyen de passer un ordre réel ni de modifier la configuration : tes
outils sont en lecture et en simulation uniquement.`, a.pair)
}

// Event est un événement poussé vers le navigateur pendant un échange.
// Kind ∈ {text, tool, tool_result, done, error}.
type Event struct {
	Kind string
	Data string
}

// Stream exécute la BOUCLE AGENTIQUE pour une conversation donnée et pousse les
// événements au fil de l'eau via emit.
//
// history contient l'historique complet (tours utilisateur + assistant
// précédents) ; le dernier message est la nouvelle question de l'utilisateur.
//
// Principe de la boucle : on appelle le modèle ; s'il répond du texte, on le
// streame ; s'il demande un/des outil(s) (stop_reason = tool_use), on les
// exécute, on renvoie les résultats, et on relance — jusqu'à ce qu'il produise
// une réponse finale sans appel d'outil.
func (a *Agent) Stream(ctx context.Context, history []anthropic.MessageParam, emit func(Event)) {
	messages := history

	// Suivi cumulé de la consommation sur tous les appels de la boucle. Émis en
	// fin d'échange (défer) pour que l'UI/la mesure connaisse le coût réel.
	var usageIn, usageOut, usageCacheRead, usageCacheWrite int64
	defer func() {
		data, _ := json.Marshal(map[string]int64{
			"input":       usageIn,
			"output":      usageOut,
			"cache_read":  usageCacheRead,
			"cache_write": usageCacheWrite,
		})
		emit(Event{Kind: "usage", Data: string(data)})
	}()

	for turn := 0; turn < maxTurns; turn++ {
		stream := a.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
			Model:     model,
			MaxTokens: maxTokens,
			// Breakpoint de cache sur le bloc system : l'ordre de rendu étant
			// tools -> system -> messages, cela met en cache le préfixe stable
			// (outils + system), relu à ~0,1x sur les appels suivants de la boucle.
			System: []anthropic.TextBlockParam{{
				Text:         a.systemPrompt(),
				CacheControl: anthropic.NewCacheControlEphemeralParam(),
			}},
			Tools:    toolDefs(),
			Messages: messages,
		})

		// On accumule les événements de streaming en un Message complet, tout en
		// poussant les deltas de texte vers le navigateur au fur et à mesure.
		acc := anthropic.Message{}
		for stream.Next() {
			ev := stream.Current()
			if err := acc.Accumulate(ev); err != nil {
				emit(Event{Kind: "error", Data: "accumulation : " + err.Error()})
				return
			}
			if delta, ok := ev.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
				if td, ok := delta.Delta.AsAny().(anthropic.TextDelta); ok {
					emit(Event{Kind: "text", Data: td.Text})
				}
			}
		}
		if err := stream.Err(); err != nil {
			emit(Event{Kind: "error", Data: err.Error()})
			return
		}

		usageIn += acc.Usage.InputTokens
		usageOut += acc.Usage.OutputTokens
		usageCacheRead += acc.Usage.CacheReadInputTokens
		usageCacheWrite += acc.Usage.CacheCreationInputTokens

		// On ajoute la réponse de l'assistant à l'historique AVANT de traiter les
		// éventuels appels d'outils (l'API exige les blocs tool_use dans le fil).
		messages = append(messages, acc.ToParam())

		// Réponse finale : plus d'outil demandé, on a terminé.
		if acc.StopReason != anthropic.StopReasonToolUse {
			emit(Event{Kind: "done"})
			return
		}

		// Sinon : exécuter chaque outil et renvoyer les résultats dans un unique
		// message utilisateur (contrat de l'API : tous les tool_result groupés).
		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range acc.Content {
			tu, ok := block.AsAny().(anthropic.ToolUseBlock)
			if !ok {
				continue
			}
			emit(Event{Kind: "tool", Data: tu.Name + " " + compact(tu.JSON.Input.Raw())})
			result, isErr := a.dispatch(tu.Name, tu.JSON.Input.Raw())
			emit(Event{Kind: "tool_result", Data: summarize(tu.Name, isErr)})
			toolResults = append(toolResults, anthropic.NewToolResultBlock(tu.ID, result, isErr))
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	emit(Event{Kind: "error", Data: fmt.Sprintf("limite de %d tours atteinte", maxTurns)})
}

// compact réduit un JSON d'arguments sur une ligne lisible (pour l'affichage de
// l'appel d'outil dans l'UI). Tronque si trop long.
func compact(raw string) string {
	s := strings.Join(strings.Fields(raw), " ")
	if len(s) > 160 {
		s = s[:157] + "…"
	}
	return s
}

// summarize produit un libellé court du résultat d'outil pour l'UI.
func summarize(name string, isErr bool) string {
	if isErr {
		return name + " : erreur"
	}
	return name + " : ok"
}
