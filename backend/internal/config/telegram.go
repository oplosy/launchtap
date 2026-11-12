package config

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	// Bot API token shape "<bot id>:<secret>".
	telegramTokenPattern = regexp.MustCompile(`^[0-9]{5,16}:[A-Za-z0-9_-]{30,64}$`)
	// Telegram bot usernames: 5-32 characters, letters, digits and underscores, ending in "bot".
	telegramUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{1,28}[Bb][Oo][Tt]$`)
)

// TelegramConfig is the cmd/telegram worker configuration
// (docs/specs/2026-11-12-telegram-alerts-design.md). BotToken is a secret and never appears in
// error text.
type TelegramConfig struct {
	ChainID      uint64 `env:"CHAIN_ID"`
	DeploymentID string `env:"DEPLOYMENT_ID"`
	DatabaseURL  string `env:"DATABASE_URL"`
	LogLevel     string `env:"LOG_LEVEL"`
	BotToken     string `env:"TELEGRAM_BOT_TOKEN"`
	BotUsername  string `env:"TELEGRAM_BOT_USERNAME"`
	// WebOrigin is the public web origin used in alert links: https, or http on localhost.
	WebOrigin string `env:"TELEGRAM_WEB_ORIGIN"`
}

// LoadTelegram parses the worker configuration; it performs no I/O.
func LoadTelegram(getenv func(string) string) (TelegramConfig, error) {
	if getenv == nil {
		return TelegramConfig{}, &FieldError{Field: "getenv", Err: ErrMissing}
	}
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	chainID, err := requiredPositiveUint64("CHAIN_ID", get("CHAIN_ID"))
	if err != nil {
		return TelegramConfig{}, err
	}
	deploymentID := get("DEPLOYMENT_ID")
	if !deploymentIDPattern.MatchString(deploymentID) {
		return TelegramConfig{}, invalidOrMissing("DEPLOYMENT_ID", deploymentID)
	}
	databaseURL := get("DATABASE_URL")
	if err := validateURL("DATABASE_URL", databaseURL, "postgres", "postgresql"); err != nil {
		return TelegramConfig{}, err
	}
	logLevel, err := parseLogLevel(get("LOG_LEVEL"))
	if err != nil {
		return TelegramConfig{}, err
	}
	token := get("TELEGRAM_BOT_TOKEN")
	if !telegramTokenPattern.MatchString(token) {
		return TelegramConfig{}, invalidOrMissing("TELEGRAM_BOT_TOKEN", token)
	}
	username := get("TELEGRAM_BOT_USERNAME")
	if !telegramUsernamePattern.MatchString(username) {
		return TelegramConfig{}, invalidOrMissing("TELEGRAM_BOT_USERNAME", username)
	}
	origin, err := parseWebOrigin("TELEGRAM_WEB_ORIGIN", get("TELEGRAM_WEB_ORIGIN"))
	if err != nil {
		return TelegramConfig{}, err
	}
	return TelegramConfig{
		ChainID: chainID, DeploymentID: deploymentID, DatabaseURL: databaseURL, LogLevel: logLevel,
		BotToken: token, BotUsername: username, WebOrigin: origin,
	}, nil
}

// TelegramBotUsername reads the API's optional TELEGRAM_BOT_USERNAME; empty turns the Telegram
// routes off.
func TelegramBotUsername(getenv func(string) string) (string, error) {
	username := strings.TrimSpace(getenv("TELEGRAM_BOT_USERNAME"))
	if username != "" && !telegramUsernamePattern.MatchString(username) {
		return "", &FieldError{Field: "TELEGRAM_BOT_USERNAME", Err: ErrInvalid}
	}
	return username, nil
}

// parseWebOrigin accepts a bare origin: https anywhere, http only on localhost or 127.0.0.1.
func parseWebOrigin(field, value string) (string, error) {
	if err := validateURL(field, value, "https", "http"); err != nil {
		return "", err
	}
	parsed, _ := url.Parse(value)
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", &FieldError{Field: field, Err: ErrInvalid}
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
		return "", &FieldError{Field: field, Err: ErrInvalid}
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}
