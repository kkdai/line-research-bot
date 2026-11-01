// Package config loads service settings from environment variables.
//
// Variable names match the original Python deployment so the same Cloud Run
// environment (and Secret Manager bindings) can be reused unchanged.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Port                   string
	GCPProjectID           string
	GCPLocation            string // Vertex AI location for Gemini (default "global")
	AgentID                string // ADK app name / agent name prefix
	GeminiModel            string
	GCSBucket              string
	CloudTasksQueue        string
	CloudTasksLocation     string
	CloudRunServiceURL     string
	ServiceAccountEmail    string
	LineChannelSecret      string
	LineChannelAccessToken string
	UseFirestoreEmulator   bool
	FirestoreEmulatorHost  string
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	c := &Config{
		Port:                   getenv("PORT", "8080"),
		GCPProjectID:           os.Getenv("GCP_PROJECT_ID"),
		GCPLocation:            getenv("GCP_LOCATION", "global"),
		AgentID:                getenv("AGENT_ID", "research-planner"),
		GeminiModel:            getenv("GEMINI_MODEL", "gemini-3-flash-preview"),
		GCSBucket:              os.Getenv("GCS_BUCKET"),
		CloudTasksQueue:        os.Getenv("CLOUD_TASKS_QUEUE"),
		CloudTasksLocation:     os.Getenv("CLOUD_TASKS_LOCATION"),
		CloudRunServiceURL:     strings.TrimRight(os.Getenv("CLOUD_RUN_SERVICE_URL"), "/"),
		ServiceAccountEmail:    os.Getenv("SERVICE_ACCOUNT_EMAIL"),
		LineChannelSecret:      os.Getenv("LINE_CHANNEL_SECRET"),
		LineChannelAccessToken: os.Getenv("LINE_CHANNEL_ACCESS_TOKEN"),
		UseFirestoreEmulator:   strings.EqualFold(os.Getenv("USE_FIRESTORE_EMULATOR"), "true"),
		FirestoreEmulatorHost:  getenv("FIRESTORE_EMULATOR_HOST", "localhost:8081"),
	}
	if c.ServiceAccountEmail == "" && c.GCPProjectID != "" {
		c.ServiceAccountEmail = c.GCPProjectID + "-compute@developer.gserviceaccount.com"
	}
	var missing []string
	for k, v := range map[string]string{
		"GCP_PROJECT_ID":            c.GCPProjectID,
		"GCS_BUCKET":                c.GCSBucket,
		"CLOUD_TASKS_QUEUE":         c.CloudTasksQueue,
		"CLOUD_TASKS_LOCATION":      c.CloudTasksLocation,
		"LINE_CHANNEL_SECRET":       c.LineChannelSecret,
		"LINE_CHANNEL_ACCESS_TOKEN": c.LineChannelAccessToken,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return c, nil
}
