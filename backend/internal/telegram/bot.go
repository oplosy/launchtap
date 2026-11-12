package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBaseURL is the Telegram Bot API origin.
const DefaultBaseURL = "https://api.telegram.org"

const maxResponseBytes = 1 << 20

// Client calls the Telegram Bot API with net/http. The token is part of every request URL, so no
// error it returns includes the URL.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// APIError is a Bot API refusal ("ok": false).
type APIError struct {
	Status      int
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *APIError) Error() string {
	return "telegram bot api error " + strconv.Itoa(e.Code) + ": " + e.Description
}

// Update is one getUpdates entry; Message is nil for update types other than a text message.
type Update struct {
	UpdateID int64
	Message  *IncomingMessage
}

type IncomingMessage struct {
	ChatID   int64
	ChatType string
	Text     string
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c Client) call(ctx context.Context, method string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram %s: encode: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/bot"+c.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram %s: build request failed", method)
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// *url.Error embeds the URL, which holds the token; keep only the cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: read response: %w", method, err)
	}
	var decoded apiResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("telegram %s: http %d with an undecodable body", method, resp.StatusCode)
	}
	if !decoded.OK {
		apiErr := &APIError{Status: resp.StatusCode, Code: decoded.ErrorCode, Description: decoded.Description}
		if decoded.Parameters != nil && decoded.Parameters.RetryAfter > 0 {
			apiErr.RetryAfter = time.Duration(decoded.Parameters.RetryAfter) * time.Second
		}
		return apiErr
	}
	if result != nil {
		if err := json.Unmarshal(decoded.Result, result); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

// SendMessage sends plain text (no parse mode) with link previews off.
func (c Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{
		"chat_id":              chatID,
		"text":                 text,
		"link_preview_options": map[string]bool{"is_disabled": true},
	}, nil)
}

// GetUpdates long-polls for message updates from offset on.
func (c Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var raw []struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			Text string `json:"text"`
		} `json:"message"`
	}
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         int(timeout / time.Second),
		"allowed_updates": []string{"message"},
	}, &raw)
	if err != nil {
		return nil, err
	}
	updates := make([]Update, 0, len(raw))
	for _, r := range raw {
		u := Update{UpdateID: r.UpdateID}
		if r.Message != nil {
			u.Message = &IncomingMessage{ChatID: r.Message.Chat.ID, ChatType: r.Message.Chat.Type, Text: r.Message.Text}
		}
		updates = append(updates, u)
	}
	return updates, nil
}
