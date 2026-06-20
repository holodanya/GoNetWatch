package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"GoNetWatch/internal/models"
)

// TelegramNotifier sends notifications via Telegram bot to multiple chat IDs
type TelegramNotifier struct {
	botToken string
	chatIDs  []string
}

// NewTelegramNotifier creates a new TelegramNotifier instance
func NewTelegramNotifier(cfg models.TelegramConfig) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: cfg.BotToken,
		chatIDs:  cfg.ChatIDs,
	}
}

// OnStateChange sends a Telegram notification when a target's state changes
// Broadcasts to all configured chat IDs
func (tn *TelegramNotifier) OnStateChange(target models.Target, result models.MonitorResult, isUp bool) error {
	var message string

	if isUp {
		// Target came back online
		message = fmt.Sprintf("✅ *RESOLVED: %s*\n\n", target.Name)
		message += fmt.Sprintf("*Address:* `%s`\n", target.Address)
		message += fmt.Sprintf("*Status:* Back online!\n")
		message += fmt.Sprintf("*Latency:* `%dms`\n", result.Latency.Milliseconds())
	} else {
		// Target went down
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

	// Broadcast to all chat IDs
	return tn.broadcastMessage(message)
}

// OnStart sends a notification when the monitoring service starts
// Broadcasts to all configured chat IDs
func (tn *TelegramNotifier) OnStart(targetCount int) error {
	message := fmt.Sprintf("🟢 *GoNetWatch Started*\n\nMonitoring %d target(s).", targetCount)
	return tn.broadcastMessage(message)
}

// OnStop sends a notification when the monitoring service stops
// Broadcasts to all configured chat IDs
func (tn *TelegramNotifier) OnStop() error {
	message := "🔴 *GoNetWatch Stopped*\n\nMonitoring gracefully shut down."
	return tn.broadcastMessage(message)
}

// broadcastMessage sends a message to all configured chat IDs
// Logs errors for individual sends but continues broadcasting to others
func (tn *TelegramNotifier) broadcastMessage(text string) error {
	if tn.botToken == "" || len(tn.chatIDs) == 0 {
		return fmt.Errorf("telegram bot token or chat IDs are not configured")
	}

	var lastErr error
	successCount := 0

	// Send to each chat ID
	for _, chatID := range tn.chatIDs {
		err := tn.sendMessageToChatID(chatID, text)
		if err != nil {
			fmt.Printf("Error sending message to chat ID %s: %v\n", chatID, err)
			lastErr = err
		} else {
			successCount++
		}
	}

	// Report overall success if at least one chat received the message
	if successCount > 0 {
		return nil
	}

	// All sends failed
	if lastErr != nil {
		return fmt.Errorf("failed to send message to all %d chat ID(s): %w", len(tn.chatIDs), lastErr)
	}

	return nil
}

// sendMessageToChatID sends a message to a specific chat ID
func (tn *TelegramNotifier) sendMessageToChatID(chatID, text string) error {
	// Build the Telegram API endpoint
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", tn.botToken)

	// Create the request payload
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	// Marshal payload to JSON
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling telegram payload: %w", err)
	}

	// Send the HTTP POST request
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("sending telegram message: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status code %d", resp.StatusCode)
	}

	return nil
}
