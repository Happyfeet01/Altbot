package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// MetricEvent represents a single event that we want to log
type MetricEvent struct {
	Timestamp time.Time
	UserID    string // This will store the hashed user ID
	EventType string
	Details   map[string]interface{}
}

// MetricsManager handles the metrics collection and reporting with detailed logs
type MetricsManager struct {
	enabled   bool
	fileMutex sync.Mutex
	logs      []MetricEvent
	filePath  string
	ticker    *time.Ticker
	wg        sync.WaitGroup
	stopChan  chan struct{}
}

// hashUserID creates a SHA-256 hash of the user ID
func hashUserID(userID string) string {
	hasher := sha256.New()
	hasher.Write([]byte(userID))
	return hex.EncodeToString(hasher.Sum(nil))
}

// NewMetricsManager initializes a new metrics manager
func NewMetricsManager(enabled bool, filePath string, interval time.Duration) *MetricsManager {
	mm := &MetricsManager{
		enabled:  enabled,
		logs:     []MetricEvent{},
		filePath: filePath,
		ticker:   time.NewTicker(interval),
		stopChan: make(chan struct{}),
	}

	if mm.enabled {
		mm.loadAndHashExistingData()
		mm.wg.Add(1)
		go mm.run()
	}

	return mm
}

// loadAndHashExistingData loads existing data and ensures all user IDs are hashed
func (mm *MetricsManager) loadAndHashExistingData() {
	mm.fileMutex.Lock()
	defer mm.fileMutex.Unlock()

	if _, err := os.Stat(mm.filePath); os.IsNotExist(err) {
		return
	}

	file, err := os.ReadFile(mm.filePath)
	if err != nil {
		log.Printf("Error reading metrics file: %v", err)
		return
	}

	var existingLogs []MetricEvent
	if err := json.Unmarshal(file, &existingLogs); err != nil {
		log.Printf("Error parsing metrics file: %v", err)
		return
	}

	needsRehash := false
	for _, event := range existingLogs {
		if len(event.UserID) != 64 {
			needsRehash = true
			break
		}
	}

	if needsRehash {
		hashedLogs := make([]MetricEvent, len(existingLogs))
		for i, event := range existingLogs {
			hashedEvent := event
			hashedEvent.UserID = hashUserID(event.UserID)
			hashedLogs[i] = hashedEvent
		}
		mm.logs = hashedLogs

		mm.saveToFile(false)
	} else {
		mm.logs = existingLogs
	}
}

// logEvent logs an event with its details
func (mm *MetricsManager) logEvent(userID, eventType string, details map[string]interface{}) {
	if !mm.enabled {
		return
	}

	event := MetricEvent{
		Timestamp: time.Now(),
		UserID:    hashUserID(userID),
		EventType: eventType,
		Details:   details,
	}

	mm.fileMutex.Lock()
	mm.logs = append(mm.logs, event)
	mm.fileMutex.Unlock()
}

// logRequest logs a user request
func (mm *MetricsManager) logRequest(userID string) {
	mm.logEvent(userID, "request", nil)
}

func (mm *MetricsManager) logFollow(userID string) {
	mm.logEvent(userID, "follow", nil)
}

// calculatePowerConsumption calculates the power consumption in Wh based on processing time and GPU watts
func calculatePowerConsumption(processingTimeMs int64, gpuWatts float64) float64 {
	// Formula: Wh = (watts × processing_time_ms) ÷ (1000 × 3600)
	// Convert milliseconds to hours and multiply by watts
	return (gpuWatts * float64(processingTimeMs)) / (1000 * 3600)
}

// logSuccessfulGeneration logs a successful alt-text generation
func (mm *MetricsManager) logSuccessfulGeneration(userID, mediaType string, responseTimeMillis int64, lang string) {
	details := map[string]interface{}{
		"mediaType":    mediaType,
		"responseTime": responseTimeMillis,
		"lang":         lang,
	}

	// Add power consumption metrics if enabled and using a local model
	if config.PowerMetrics.Enabled && config.LLM.Provider != "gemini" {
		powerConsumption := calculatePowerConsumption(responseTimeMillis, config.PowerMetrics.GPUWatts)
		details["powerConsumptionKWh"] = powerConsumption
	}

	mm.logEvent(userID, "successful_generation", details)
}

// logRateLimitHit logs when a rate limit is hit
func (mm *MetricsManager) logRateLimitHit(userID string) {
	mm.logEvent(userID, "rate_limit_hit", nil)
}

func (mm *MetricsManager) logNewAccountActivity(userID string) {
	mm.logEvent(userID, "new_account_activity", nil)
}

func (mm *MetricsManager) logShadowBan(userID string) {
	mm.logEvent(userID, "shadow_ban", nil)
}

func (mm *MetricsManager) logUnBan(userID string) {
	mm.logEvent(userID, "un_ban", nil)
}

func (mm *MetricsManager) logWeeklySummary(userID string) {
	mm.logEvent(userID, "weekly_summary", nil)
}

func (mm *MetricsManager) logMissingAltText(userID string) {
	mm.logEvent(userID, "missing_alt_text", nil)
}

func (mm *MetricsManager) logAltTextReminderSent(userID string) {
	mm.logEvent(userID, "alt_text_reminder_sent", nil)
}

// logConsentRequest logs a consent request
func (mm *MetricsManager) logConsentRequest(userID string, granted bool) {
	details := map[string]interface{}{
		"granted": granted,
	}
	mm.logEvent(userID, "consent_request", details)
}

// saveToFile writes the current metrics data to a file
func (mm *MetricsManager) saveToFile(lock bool) {
	if lock {
		mm.fileMutex.Lock()
		defer mm.fileMutex.Unlock()
	}

	file, err := os.OpenFile(mm.filePath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening metrics file: %v", err)
		return
	}
	defer file.Close()

	if err := file.Truncate(0); err != nil {
		log.Printf("Error truncating metrics file: %v", err)
		return
	}

	if _, err := file.Seek(0, 0); err != nil {
		log.Printf("Error seeking in metrics file: %v", err)
		return
	}

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(mm.logs); err != nil {
		log.Printf("Error writing metrics to file: %v", err)
		return
	}

}

func (mm *MetricsManager) run() {
	defer mm.wg.Done()
	for {
		select {
		case <-mm.ticker.C:
			mm.saveToFile(true)
		case <-mm.stopChan:
			mm.ticker.Stop()
			mm.saveToFile(true)
			return
		}
	}
}

// stop terminates the background metrics manager
func (mm *MetricsManager) stop() {
	close(mm.stopChan)
	mm.wg.Wait()
}
