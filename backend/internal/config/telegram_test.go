package config

import (
	"errors"
	"strings"
	"testing"
)

func telegramEnv() map[string]string {
	return map[string]string{
		"CHAIN_ID":              "46630",
		"DEPLOYMENT_ID":         "rh-mainnet",
		"DATABASE_URL":          "postgres://u:p@db:5432/launchpad",
		"TELEGRAM_BOT_TOKEN":    " 123456789:AAH-secret_token_value_abcdefghijklmnop ",
		"TELEGRAM_BOT_USERNAME": "LaunchAlertsBot",
		"TELEGRAM_WEB_ORIGIN":   "https://launch.example",
	}
}

func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestLoadTelegram(t *testing.T) {
	t.Parallel()
	c, err := LoadTelegram(lookup(telegramEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.ChainID != 46630 || c.DeploymentID != "rh-mainnet" || c.BotToken != "123456789:AAH-secret_token_value_abcdefghijklmnop" ||
		c.BotUsername != "LaunchAlertsBot" || c.WebOrigin != "https://launch.example" || c.LogLevel != "info" {
		t.Fatalf("config = %+v", c)
	}

	local := telegramEnv()
	local["TELEGRAM_WEB_ORIGIN"] = "http://localhost:3417"
	if _, err := LoadTelegram(lookup(local)); err != nil {
		t.Fatalf("localhost origin: %v", err)
	}

	for field, bad := range map[string]string{
		"TELEGRAM_BOT_TOKEN":    "not-a-token",
		"TELEGRAM_BOT_USERNAME": "@LaunchAlertsBot",
		"TELEGRAM_WEB_ORIGIN":   "http://launch.example",
		"DEPLOYMENT_ID":         "X",
		"CHAIN_ID":              "0",
	} {
		env := telegramEnv()
		env[field] = bad
		_, err := LoadTelegram(lookup(env))
		var fieldErr *FieldError
		if !errors.As(err, &fieldErr) || fieldErr.Field != field {
			t.Fatalf("%s=%q error = %v", field, bad, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("error leaks a value: %v", err)
		}
	}
	for _, path := range []string{"https://launch.example/app", "https://launch.example?x=1"} {
		env := telegramEnv()
		env["TELEGRAM_WEB_ORIGIN"] = path
		if _, err := LoadTelegram(lookup(env)); err == nil {
			t.Fatalf("origin %q accepted", path)
		}
	}
	missing := telegramEnv()
	delete(missing, "TELEGRAM_BOT_TOKEN")
	if _, err := LoadTelegram(lookup(missing)); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing token: %v", err)
	}
}

func TestTelegramBotUsernameIsOptionalForTheAPI(t *testing.T) {
	t.Parallel()
	if name, err := TelegramBotUsername(lookup(map[string]string{})); err != nil || name != "" {
		t.Fatalf("empty = %q, %v", name, err)
	}
	if name, err := TelegramBotUsername(lookup(map[string]string{"TELEGRAM_BOT_USERNAME": " LaunchAlertsBot "})); err != nil || name != "LaunchAlertsBot" {
		t.Fatalf("set = %q, %v", name, err)
	}
	if _, err := TelegramBotUsername(lookup(map[string]string{"TELEGRAM_BOT_USERNAME": "launch alerts"})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid = %v", err)
	}
}
