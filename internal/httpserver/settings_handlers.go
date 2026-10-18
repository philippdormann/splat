package httpserver

import (
	"net/http"
	"strings"

	"errgotrack/internal/notifications"
	"errgotrack/internal/webhooks"
)

type webhookDisplay struct {
	URLMasked  string
	Enabled    bool
	TestResult string
	TestOK     bool
}

type settingsPageData struct {
	PageBase
	Telegram         notifications.TelegramConfig
	BotTokenMasked   string
	TelegramSaved    bool
	TelegramTest     string
	TelegramTestOK   bool
	Webhooks         map[string]webhookDisplay
	TraceURLTemplate string
	TraceSaved       bool
}

var webhookChannels = []string{"slack", "discord", "teams", "sheets"}

func isWebhookChannel(channel string) bool {
	for _, c := range webhookChannels {
		if c == channel {
			return true
		}
	}
	return false
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("•", len(token))
	}
	return token[:4] + strings.Repeat("•", len(token)-8) + token[len(token)-4:]
}

// buildSettingsData reloads every notification channel's config fresh from
// the database. Called at the start of every settings handler so a save on
// one channel doesn't clobber what's displayed for the others.
func (s *Server) buildSettingsData(w http.ResponseWriter, r *http.Request) settingsPageData {
	cfg, err := s.Notifications.GetTelegramConfig()
	if err != nil {
		s.Logger.Error("load telegram config", "error", err)
	}
	traceTmpl, err := s.Tracing.GetURLTemplate()
	if err != nil {
		s.Logger.Error("load tracing config", "error", err)
	}

	webhookData := make(map[string]webhookDisplay, len(webhookChannels))
	for _, channel := range webhookChannels {
		wc, err := s.Notifications.GetWebhookConfig(channel)
		if err != nil {
			s.Logger.Error("load webhook config", "error", err, "channel", channel)
		}
		webhookData[channel] = webhookDisplay{URLMasked: maskToken(wc.URL), Enabled: wc.Enabled}
	}

	return settingsPageData{
		PageBase: s.pageBase(w, r, "settings"), Telegram: cfg, BotTokenMasked: maskToken(cfg.BotToken),
		Webhooks: webhookData, TraceURLTemplate: traceTmpl,
	}
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	s.render.Page(w, "settings", s.buildSettingsData(w, r))
}

func (s *Server) settingsSaveTracing(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	tmpl := r.FormValue("trace_url_template")
	if err := s.Tracing.SetURLTemplate(tmpl); err != nil {
		s.Logger.Error("save tracing config", "error", err)
	}
	data := s.buildSettingsData(w, r)
	data.TraceSaved = true
	s.render.Page(w, "settings", data)
}

func (s *Server) settingsSaveTelegram(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	cfg, _ := s.Notifications.GetTelegramConfig()
	// A masked token submitted unchanged (contains the bullet mask) means
	// "keep the existing secret" - only overwrite it if the operator typed
	// a real new value.
	if token := r.FormValue("bot_token"); token != "" && !strings.Contains(token, "•") {
		cfg.BotToken = token
	}
	cfg.ChatID = r.FormValue("chat_id")
	cfg.Enabled = r.FormValue("enabled") == "on"
	if err := s.Notifications.SetTelegramConfig(cfg); err != nil {
		s.Logger.Error("save telegram config", "error", err)
	}
	data := s.buildSettingsData(w, r)
	data.TelegramSaved = true
	s.render.Page(w, "settings", data)
}

func (s *Server) settingsTestTelegram(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	err := s.Notifications.SendTest(r.Context())
	data := s.buildSettingsData(w, r)
	data.TelegramTest = "Test notification sent."
	data.TelegramTestOK = true
	if err != nil {
		data.TelegramTest = "Failed to send: " + err.Error()
		data.TelegramTestOK = false
	}
	s.render.Page(w, "settings", data)
}

func (s *Server) settingsSaveWebhook(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	channel := r.PathValue("channel")
	if !isWebhookChannel(channel) {
		http.NotFound(w, r)
		return
	}
	cfg, _ := s.Notifications.GetWebhookConfig(channel)
	var validationErr error
	if url := r.FormValue("webhook_url"); url != "" && !strings.Contains(url, "•") {
		if err := webhooks.ValidateURL(url); err != nil {
			validationErr = err
		} else {
			cfg.URL = url
		}
	}
	if validationErr == nil {
		cfg.Enabled = r.FormValue("enabled") == "on"
		if err := s.Notifications.SetWebhookConfig(channel, cfg); err != nil {
			s.Logger.Error("save webhook config", "error", err, "channel", channel)
		}
	}
	data := s.buildSettingsData(w, r)
	wd := data.Webhooks[channel]
	wd.TestResult = ""
	if validationErr != nil {
		wd.TestResult = "Not saved: " + validationErr.Error()
		wd.TestOK = false
	}
	data.Webhooks[channel] = wd
	s.render.Page(w, "settings", data)
}

func (s *Server) settingsTestWebhook(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	channel := r.PathValue("channel")
	if !isWebhookChannel(channel) {
		http.NotFound(w, r)
		return
	}
	err := s.Notifications.SendTestWebhook(r.Context(), channel)
	data := s.buildSettingsData(w, r)
	wd := data.Webhooks[channel]
	wd.TestResult = "Test notification sent."
	wd.TestOK = true
	if err != nil {
		wd.TestResult = "Failed to send: " + err.Error()
		wd.TestOK = false
	}
	data.Webhooks[channel] = wd
	s.render.Page(w, "settings", data)
}
