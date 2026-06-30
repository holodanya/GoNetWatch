package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"GoNetWatch/internal/models"
)

const telegramQueueSize = 100

// TelegramNotifier sends notifications via Telegram bot to multiple chat IDs.
type TelegramNotifier struct {
	botToken string
	chatIDs  []string
	messages chan string
}

// NewTelegramNotifier creates a new TelegramNotifier instance.
func NewTelegramNotifier(cfg models.TelegramConfig) *TelegramNotifier {
	tn := &TelegramNotifier{
		botToken: strings.TrimSpace(cfg.BotToken),
		chatIDs:  normalizeChatIDs(cfg.ChatIDs),
		messages: make(chan string, telegramQueueSize),
	}

	go tn.dispatch()

	return tn
}

// OnStateChange sends a Telegram notification when a target's state changes.
func (tn *TelegramNotifier) OnStateChange(target models.Target, result models.MonitorResult, isUp bool) error {
	if !tn.enabled() {
		return nil
	}

	var message string

	if isUp {
		message = fmt.Sprintf("✅ *RESOLVED: %s*\n\n", target.Name)
		message += fmt.Sprintf("*Address:* `%s`\n", target.Address)
		message += fmt.Sprintf("*Status:* Back online!\n")
		message += fmt.Sprintf("*Latency:* `%dms`\n", result.Latency.Milliseconds())
	} else {
		message = fmt.Sprintf("🚨 *DOWN: %s*\n\n", target.Name)
		message += fmt.Sprintf("*Address:* `%s`\n", target.Address)
		if result.Error != "" {
			message += fmt.Sprintf("*Error:* %s\n", result.Error)
		}
		if result.Code > 0 {
			message += fmt.Sprintf("*Status Code:* `%d`\n", result.Code)
		}
		message += fmt.Sprintf("*Latency:* `%dms`\n", result.Latency.Milliseconds())
	}

	tn.enqueue(message)
	return nil
}

// OnStart sends a notification when the monitoring service starts.
func (tn *TelegramNotifier) OnStart(targetCount int) error {
	if !tn.enabled() {
		return nil
	}

	message := fmt.Sprintf("🟢 *GoNetWatch Started*\n\nMonitoring %d target(s).", targetCount)
	tn.enqueue(message)
	return nil
}

// OnStop sends a notification when the monitoring service stops.
func (tn *TelegramNotifier) OnStop() error {
	if !tn.enabled() {
		return nil
	}

	message := "🔴 *GoNetWatch Stopped*\n\nMonitoring gracefully shut down."
	tn.enqueue(message)
	return nil
}

func normalizeChatIDs(chatIDs []string) []string {
	normalized := make([]string, 0, len(chatIDs))
	for _, chatID := range chatIDs {
		chatID = strings.TrimSpace(chatID)
		if chatID != "" {
			normalized = append(normalized, chatID)
		}
	}
	return normalized
}

func (tn *TelegramNotifier) enabled() bool {
	return tn != nil && tn.botToken != "" && len(tn.chatIDs) > 0 && tn.messages != nil
}

func (tn *TelegramNotifier) enqueue(message string) {
	select {
	case tn.messages <- message:
	default:
		slog.Warn("Telegram notification queue is full; dropping message")
	}
}

func (tn *TelegramNotifier) dispatch() {
	for message := range tn.messages {
		if err := tn.broadcastMessage(message); err != nil {
			slog.Error("Failed to send Telegram notification", slog.String("error", err.Error()))
		}
	}
}

// broadcastMessage sends a message to all configured chat IDs.
// Logs per-chat errors but continues; succeeds if at least one chat received the message.
func (tn *TelegramNotifier) broadcastMessage(text string) error {
	if !tn.enabled() {
		return nil
	}

	var lastErr error
	successCount := 0

	for _, chatID := range tn.chatIDs {
		err := tn.sendMessageToChatID(chatID, text)
		if err != nil {
			slog.Error("Failed to send Telegram alert",
				slog.String("chat_id", chatID),
				slog.String("error", err.Error()))
			lastErr = err
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		slog.Info("Telegram alert sent",
			slog.Int("delivered", successCount),
			slog.Int("chats", len(tn.chatIDs)))
		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("failed to send message to all %d chat ID(s): %w", len(tn.chatIDs), lastErr)
	}

	return nil
}

// sendMessageToChatID sends a message to a specific chat ID.
func (tn *TelegramNotifier) sendMessageToChatID(chatID, text string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tn.botToken)

	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling telegram payload: %w", err)
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("sending telegram message: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status code %d", resp.StatusCode)
	}

	return nil
}
