package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"bot/internal/logger"
)

// ===============================
// DASHBOARD INTERACTIF TELEGRAM
// ===============================
//
// Au lieu de subir des notifications push, l'utilisateur interroge le bot à la
// demande. On utilise le « long-polling » (getUpdates) : c'est le bot qui appelle
// api.telegram.org en sortant, aucune connexion entrante n'est requise — idéal
// pour un serveur local non exposé sur internet.
//
// Un unique message éditable fait office de mini-dashboard : les boutons inline
// déclenchent un editMessageText qui réécrit le message sur place (status →
// cycles → pnl), sans spammer la conversation.

// StatusSnapshot est l'instantané affiché par /status.
type StatusSnapshot struct {
	Version      string // version du binaire (injectée par make release)
	Exchange     string
	Pair         string
	Price        string // déjà formaté selon la précision du marché
	RSI          string // formaté, ou "n/a"
	RSITimeframe string
	ActiveCycles int
	OpenCycles   int // cycles dont l'achat est rempli, en attente de vente
	TotalProfit  float64
	AvgProfit    float64
	Quote        string
	Paused       bool
	UpdatedAt    time.Time
	Uptime       string // durée depuis le démarrage, ou "" si inconnu
	LastCheckAgo string // temps écoulé depuis le dernier price-check, ou "" si aucun
	ErrorMsg     string // dernière erreur récente, ou "" si aucune
	ErrorAgo     string // temps écoulé depuis cette erreur
}

// CycleView est une ligne de la vue /cycles.
type CycleView struct {
	ID       int
	Status   string
	Amount   string
	BuyPrice string
	Target   string
	Age      string
}

// PnLSnapshot est l'instantané de la vue /pnl.
type PnLSnapshot struct {
	Completed   int
	TotalProfit float64
	AvgProfit   float64
	Quote       string
}

// BalanceLine est un solde d'actif dans la vue /balance.
type BalanceLine struct {
	Asset  string
	Amount string
	Locked string // montant bloqué dans des ordres ouverts, ou "" si aucun
	Value  string // valorisation en devise de cotation (total, dont bloqué), ou "" si inconnue
}

// BalanceSnapshot est l'instantané de la vue /balance.
type BalanceSnapshot struct {
	Exchange string
	Lines    []BalanceLine
	Total    string // total valorisé, ou "" si rien de valorisable
}

// Dashboard est la source de données et de contrôle fournie par le bot.
// L'interface est définie ici pour découpler le package telegram du package bot
// (qui importe déjà telegram pour les notifications push).
type Dashboard interface {
	Status() (StatusSnapshot, error)
	Cycles() ([]CycleView, error)
	PnL() (PnLSnapshot, error)
	Balance() (BalanceSnapshot, error)
	Pause() error
	Resume() error
	// BuyNow déclenche un achat manuel immédiat et retourne un résumé de l'ordre posé.
	BuyNow() (string, error)
}

// Handle permet de piloter le dashboard depuis l'extérieur après son démarrage
// (ex. notifier un arrêt propre). Toutes ses méthodes sont nil-safe.
type Handle struct {
	b *dashboardBot
}

// StartPolling démarre la boucle interactive en tâche de fond. Elle s'arrête
// quand ctx est annulé. Renvoie un Handle (nil-safe) pour notifier l'arrêt.
// No-op si TELEGRAM != "1" ou si le token manque.
func StartPolling(ctx context.Context, dash Dashboard) *Handle {
	if os.Getenv("TELEGRAM") != "1" {
		return nil
	}

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		logger.Warnf("Dashboard Telegram désactivé : TELEGRAM_BOT_TOKEN manquant")
		return nil
	}

	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if chatID == "" {
		// Mode découverte : on ne pilote rien, on logge juste le chat_id des messages
		// reçus pour que l'utilisateur puisse renseigner TELEGRAM_CHAT_ID.
		logger.Warnf("TELEGRAM_CHAT_ID non défini : mode découverte actif — envoyez un message au bot pour récupérer votre chat_id (affiché dans les logs et renvoyé sur Telegram)")
	}

	b := &dashboardBot{
		token:  token,
		chatID: chatID,
		dash:   dash,
		client: &http.Client{Timeout: 60 * time.Second},
	}
	go b.loop(ctx)
	return &Handle{b: b}
}

// NotifyStopped transforme le dernier message-dashboard en bannière « arrêté »
// (édition sur place), ou envoie un nouveau message si aucun dashboard n'a été
// ouvert pendant la session. À appeler lors d'un arrêt propre du bot.
func (h *Handle) NotifyStopped() {
	if h == nil || h.b == nil || h.b.chatID == "" {
		return
	}

	text := "🛑 simple-bot — Bot arrêté proprement\n⏱ " + time.Now().Format("2006-01-02 15:04:05")
	// On ne garde qu'un bouton « Rafraîchir » : inerte tant que le bot est down,
	// il permet de ré-afficher l'état dès qu'il est relancé (sans retaper /status).
	kb := refreshKeyboard()
	if id := h.b.lastMessageID.Load(); id > 0 {
		h.b.edit(int(id), text, &kb)
	} else {
		h.b.send(text, &kb)
	}
}

type dashboardBot struct {
	token  string
	chatID string // seul ce chat est autorisé à piloter le bot
	dash   Dashboard
	client *http.Client
	offset int
	// lastMessageID : id du dernier message-dashboard envoyé/édité, pour pouvoir
	// le transformer en bannière « arrêté » au shutdown.
	lastMessageID atomic.Int64
}

// ===============================
// BOUCLE DE POLLING
// ===============================

func (b *dashboardBot) loop(ctx context.Context) {
	logger.Infof("Dashboard Telegram actif (long-polling)")
	// Déclare les commandes → active le bouton « Menu » de la barre de saisie.
	b.setMyCommands()
	for {
		if ctx.Err() != nil {
			logger.Infof("Dashboard Telegram arrêté")
			return
		}

		updates, err := b.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warnf("Telegram getUpdates : %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		for _, u := range updates {
			b.offset = u.UpdateID + 1
			b.handleUpdate(u)
		}
	}
}

func (b *dashboardBot) getUpdates(ctx context.Context) ([]tgUpdate, error) {
	payload := map[string]any{
		"timeout":         50,
		"offset":          b.offset,
		"allowed_updates": []string{"message", "callback_query"},
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", b.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK          bool       `json:"ok"`
		ErrorCode   int        `json:"error_code"`
		Description string     `json:"description"`
		Result      []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if !result.OK {
		// Ex. 409 Conflict : un autre process interroge getUpdates avec le même
		// token (un seul consommateur autorisé par bot).
		return nil, fmt.Errorf("%d %s", result.ErrorCode, result.Description)
	}
	return result.Result, nil
}

// ===============================
// ROUTAGE DES MESSAGES & BOUTONS
// ===============================

func (b *dashboardBot) handleUpdate(u tgUpdate) {
	// Pas de chat_id configuré : on aide juste l'utilisateur à le découvrir.
	if b.chatID == "" {
		b.discover(u)
		return
	}

	switch {
	case u.Message != nil:
		if fmt.Sprint(u.Message.Chat.ID) != b.chatID {
			return // chat non autorisé : on ignore silencieusement
		}
		b.handleCommand(u.Message.Text)

	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		cq := u.CallbackQuery
		if fmt.Sprint(cq.Message.Chat.ID) != b.chatID {
			return
		}
		b.handleCallback(cq.Data, cq.Message.MessageID)
		b.answerCallback(cq.ID)
	}
}

// discover répond et logge le chat_id de l'expéditeur quand TELEGRAM_CHAT_ID
// n'est pas encore configuré, pour aider à le renseigner.
func (b *dashboardBot) discover(u tgUpdate) {
	var id int64
	switch {
	case u.Message != nil:
		id = u.Message.Chat.ID
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		id = u.CallbackQuery.Message.Chat.ID
	default:
		return
	}

	chatID := fmt.Sprint(id)
	logger.Infof("Telegram : chat_id = %s — ajoutez TELEGRAM_CHAT_ID=%s au .env de l'instance puis redémarrez", chatID, chatID)
	b.sendTo(chatID, fmt.Sprintf("Ton chat_id est : %s\nAjoute TELEGRAM_CHAT_ID=%s au .env de l'instance puis redémarre le bot.", chatID, chatID))
}

func (b *dashboardBot) handleCommand(text string) {
	cmd := strings.TrimSpace(text)
	// Retire un éventuel argument ou suffixe @botname (ex. "/status@mybot").
	if i := strings.IndexAny(cmd, " @"); i >= 0 {
		cmd = cmd[:i]
	}

	switch cmd {
	case "/start", "/status", "/menu":
		b.sendView("status")
	case "/cycles":
		b.sendView("cycles")
	case "/pnl":
		b.sendView("pnl")
	case "/balance":
		b.sendView("balance")
	case "/pause":
		_ = b.dash.Pause()
		b.sendView("status")
	case "/resume":
		_ = b.dash.Resume()
		b.sendView("status")
	default:
		b.send("Commandes : /status · /cycles · /pnl · /pause · /resume", nil)
	}
}

func (b *dashboardBot) handleCallback(data string, messageID int) {
	// Le message édité devient le dashboard courant (pour la bannière d'arrêt).
	b.lastMessageID.Store(int64(messageID))

	switch data {
	case "pause":
		_ = b.dash.Pause()
		data = "status"
	case "resume":
		_ = b.dash.Resume()
		data = "status"

	case "buy":
		// Étape 1/2 : on demande confirmation (un achat manuel engage de l'argent réel).
		kb := confirmBuyKeyboard()
		b.edit(messageID, "🛒 Confirmer un achat manuel ?\n\nUn ordre d'achat maker (taille dynamique) sera posé sous le prix courant, hors condition RSI et hors cooldown.\n⚠️ Argent réel.", &kb)
		return

	case "buy_confirm":
		// Étape 2/2 : exécution effective.
		kb := backKeyboard()
		msg, err := b.dash.BuyNow()
		if err != nil {
			b.edit(messageID, "⚠️ Achat impossible : "+err.Error(), &kb)
			return
		}
		b.edit(messageID, msg, &kb)
		return
	}

	// "annuler" et tout le reste retombent sur le rendu de vue (status par défaut).
	text, kb := b.render(data)
	b.edit(messageID, text, kb)
}

// parseMessageID extrait result.message_id d'une réponse sendMessage.
func parseMessageID(data []byte) int {
	var r struct {
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	_ = json.Unmarshal(data, &r)
	return r.Result.MessageID
}

func (b *dashboardBot) sendView(view string) {
	text, kb := b.render(view)
	b.postDashboard(text, kb)
}

// render assemble le texte et le clavier d'une vue donnée.
func (b *dashboardBot) render(view string) (string, *inlineKeyboard) {
	switch view {
	case "cycles":
		cs, err := b.dash.Cycles()
		if err != nil {
			kb := backKeyboard()
			return "⚠️ Erreur : " + err.Error(), &kb
		}
		kb := backKeyboard()
		return renderCycles(cs), &kb

	case "pnl":
		p, err := b.dash.PnL()
		if err != nil {
			kb := backKeyboard()
			return "⚠️ Erreur : " + err.Error(), &kb
		}
		kb := backKeyboard()
		return renderPnL(p), &kb

	case "balance":
		bal, err := b.dash.Balance()
		if err != nil {
			kb := backKeyboard()
			return "⚠️ Erreur : " + err.Error(), &kb
		}
		kb := backKeyboard()
		return renderBalance(bal), &kb

	default: // "status"
		s, err := b.dash.Status()
		if err != nil {
			return "⚠️ Erreur : " + err.Error(), nil
		}
		kb := mainKeyboard(s.Paused)
		return renderStatus(s), &kb
	}
}

// ===============================
// RENDU DES VUES
// ===============================

func renderStatus(s StatusSnapshot) string {
	state := "🟢 Actif"
	switch {
	case s.Paused:
		state = "⏸ En pause"
	case s.ErrorMsg != "":
		state = "⚠️ Erreurs récentes"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📊 simple-bot %s — %s %s\n", s.Version, s.Exchange, s.Pair)
	fmt.Fprintf(&b, "%s\n", state)
	if s.ErrorMsg != "" {
		fmt.Fprintf(&b, "↳ il y a %s : %s\n", s.ErrorAgo, s.ErrorMsg)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Prix : %s %s\n", s.Price, s.Quote)
	if s.RSI != "n/a" {
		fmt.Fprintf(&b, "RSI(%s) : %s\n", s.RSITimeframe, s.RSI)
	}
	fmt.Fprintf(&b, "Cycles actifs : %d   (achat rempli : %d)\n", s.ActiveCycles, s.OpenCycles)
	fmt.Fprintf(&b, "PnL réalisé : %+.2f %s   (moy. %+.2f)\n", s.TotalProfit, s.Quote, s.AvgProfit)

	b.WriteString("\n")
	if s.LastCheckAgo != "" {
		fmt.Fprintf(&b, "💓 Dernier check : il y a %s\n", s.LastCheckAgo)
	}
	if s.Uptime != "" {
		fmt.Fprintf(&b, "⏳ Uptime : %s\n", s.Uptime)
	}
	fmt.Fprintf(&b, "⏱ %s", s.UpdatedAt.Format("15:04:05"))
	return b.String()
}

func renderCycles(cs []CycleView) string {
	if len(cs) == 0 {
		return "📈 Cycles actifs\n\nAucun cycle actif."
	}

	var b strings.Builder
	b.WriteString("📈 Cycles actifs")
	for _, c := range cs {
		fmt.Fprintf(&b, "\n\n#%d · %s\n  %s @ %s → 🎯 %s  (%s)",
			c.ID, c.Status, c.Amount, c.BuyPrice, c.Target, c.Age)
	}
	return b.String()
}

func renderBalance(s BalanceSnapshot) string {
	if len(s.Lines) == 0 {
		return "💼 Balance — " + s.Exchange + "\n\nAucun solde."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "💼 Balance — %s\n", s.Exchange)
	for _, l := range s.Lines {
		fmt.Fprintf(&b, "\n%s : %s", l.Asset, l.Amount)
		if l.Locked != "" {
			fmt.Fprintf(&b, " (dont %s bloqué en ordres)", l.Locked)
		}
		if l.Value != "" {
			fmt.Fprintf(&b, "  (%s)", l.Value)
		}
	}
	if s.Total != "" {
		fmt.Fprintf(&b, "\n─────────────\nTotal ≈ %s", s.Total)
	}
	return b.String()
}

func renderPnL(p PnLSnapshot) string {
	var b strings.Builder
	b.WriteString("💰 PnL réalisé\n\n")
	fmt.Fprintf(&b, "Cycles terminés : %d\n", p.Completed)
	fmt.Fprintf(&b, "Profit total : %+.2f %s\n", p.TotalProfit, p.Quote)
	fmt.Fprintf(&b, "Profit moyen : %+.2f %s", p.AvgProfit, p.Quote)
	return b.String()
}

// ===============================
// CLAVIERS INLINE
// ===============================

func mainKeyboard(paused bool) inlineKeyboard {
	pauseBtn := inlineButton{Text: "⏸ Pause", CallbackData: "pause"}
	if paused {
		pauseBtn = inlineButton{Text: "▶️ Reprendre", CallbackData: "resume"}
	}
	return inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "🔄 Rafraîchir", CallbackData: "status"}},
		{{Text: "📈 Cycles", CallbackData: "cycles"}, {Text: "💰 PnL", CallbackData: "pnl"}, {Text: "💼 Balance", CallbackData: "balance"}},
		{{Text: "🛒 Acheter", CallbackData: "buy"}, pauseBtn},
	}}
}

// confirmBuyKeyboard : confirmation à deux temps d'un achat manuel (argent réel).
func confirmBuyKeyboard() inlineKeyboard {
	return inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "✅ Confirmer l'achat", CallbackData: "buy_confirm"}, {Text: "❌ Annuler", CallbackData: "annuler"}},
	}}
}

func backKeyboard() inlineKeyboard {
	return inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "◀️ Retour", CallbackData: "status"}},
	}}
}

func refreshKeyboard() inlineKeyboard {
	return inlineKeyboard{InlineKeyboard: [][]inlineButton{
		{{Text: "🔄 Rafraîchir", CallbackData: "status"}},
	}}
}

// ===============================
// APPELS API BAS NIVEAU
// ===============================

// send poste un nouveau message et retourne son message_id (0 si échec). Le
// suivi du dashboard courant (lastMessageID) est laissé à l'appelant : seul
// postDashboard mémorise l'id, un simple message d'aide ne doit pas l'écraser.
func (b *dashboardBot) send(text string, kb *inlineKeyboard) int {
	payload := map[string]any{"chat_id": b.chatID, "text": text}
	if kb != nil {
		payload["reply_markup"] = kb
	}
	data, err := b.apiCall("sendMessage", payload)
	if err != nil {
		logger.Errorf("Telegram sendMessage : %v", err)
		return 0
	}
	return parseMessageID(data)
}

// postDashboard poste le dashboard tout en bas de la conversation puis supprime
// l'exemplaire précédent. Les notifications push (clôtures d'ordres, erreurs)
// s'insèrent en bas et font remonter le message-dashboard hors de vue ; le
// re-poster à chaque commande le ramène sous la main, et supprimer l'ancien
// évite d'empiler des dashboards périmés dans l'historique.
func (b *dashboardBot) postDashboard(text string, kb *inlineKeyboard) {
	old := b.lastMessageID.Load()
	id := b.send(text, kb)
	if id > 0 {
		b.lastMessageID.Store(int64(id))
	}
	// On supprime l'ancien seulement après avoir posté le nouveau (si l'envoi
	// échoue, on garde au moins le précédent).
	if old > 0 && int(old) != id {
		b.deleteMessage(int(old))
	}
}

// sendTo envoie un message à un chat arbitraire (utilisé en mode découverte,
// quand le chat autorisé n'est pas encore connu).
func (b *dashboardBot) sendTo(chatID, text string) {
	if _, err := b.apiCall("sendMessage", map[string]any{"chat_id": chatID, "text": text}); err != nil {
		logger.Errorf("Telegram sendMessage : %v", err)
	}
}

func (b *dashboardBot) edit(messageID int, text string, kb *inlineKeyboard) {
	payload := map[string]any{"chat_id": b.chatID, "message_id": messageID, "text": text}
	if kb != nil {
		payload["reply_markup"] = kb
	}
	if _, err := b.apiCall("editMessageText", payload); err != nil {
		// Rafraîchir une vue inchangée renvoie « message is not modified » : sans gravité.
		if !strings.Contains(err.Error(), "not modified") {
			logger.Errorf("Telegram editMessageText : %v", err)
		}
	}
}

func (b *dashboardBot) answerCallback(id string) {
	_, _ = b.apiCall("answerCallbackQuery", map[string]any{"callback_query_id": id})
}

// deleteMessage supprime un message de la conversation. Utilisé pour retirer
// l'ancien dashboard une fois qu'un nouveau a été posté plus bas.
func (b *dashboardBot) deleteMessage(messageID int) {
	if _, err := b.apiCall("deleteMessage", map[string]any{"chat_id": b.chatID, "message_id": messageID}); err != nil {
		// Message déjà supprimé ou trop ancien (>48 h) : sans gravité.
		logger.Debugf("Telegram deleteMessage : %v", err)
	}
}

// setMyCommands déclare la liste des commandes du bot. Effet visible : Telegram
// affiche un bouton « Menu » à gauche de la barre de saisie (toujours au bas de
// l'écran, jamais emporté par le défilement), qui déroule ces commandes et
// permet de ré-afficher le dashboard d'un tap.
func (b *dashboardBot) setMyCommands() {
	cmds := []map[string]string{
		{"command": "status", "description": "État du bot"},
		{"command": "cycles", "description": "Cycles actifs"},
		{"command": "pnl", "description": "PnL réalisé"},
		{"command": "balance", "description": "Soldes du compte"},
		{"command": "pause", "description": "Mettre le bot en pause"},
		{"command": "resume", "description": "Reprendre le bot"},
	}
	if _, err := b.apiCall("setMyCommands", map[string]any{"commands": cmds}); err != nil {
		logger.Warnf("Telegram setMyCommands : %v", err)
	}
}

func (b *dashboardBot) apiCall(method string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/%s", b.token, method)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// Le corps de réponse Telegram contient la description de l'erreur.
		return data, fmt.Errorf("telegram %s : status %d (%s)", method, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// ===============================
// TYPES API TELEGRAM
// ===============================

type tgUpdate struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		MessageID int `json:"message_id"`
		Chat      struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
	CallbackQuery *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message *struct {
			MessageID int `json:"message_id"`
			Chat      struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

type inlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type inlineKeyboard struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}
