/*
 * File: db.go
 * Project: flip-ai
 * Created: 2026-04-29
 */

package services

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"flip-ai/internal/models"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func InitDB() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/history.db"
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		_ = os.MkdirAll(dir, 0755)
	}

	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Create tables
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		conversation_id TEXT,
		msg_id TEXT UNIQUE,
		role TEXT,
		content TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_conv ON messages(conversation_id);

	CREATE TABLE IF NOT EXISTS sessions (
		fingerprint TEXT PRIMARY KEY,
		conversation_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS agent_states (
		id TEXT PRIMARY KEY,
		goal TEXT,
		status TEXT,
		state_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS web_chat_sessions (
		provider TEXT NOT NULL,
		session_key TEXT NOT NULL,
		chat_id TEXT NOT NULL DEFAULT '',
		parent_message_id TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		rollover_count INTEGER NOT NULL DEFAULT 0,
		estimated_tokens INTEGER NOT NULL DEFAULT 0,
		client_message_count INTEGER NOT NULL DEFAULT 0,
		last_message_hash TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(provider, session_key)
	);
	CREATE INDEX IF NOT EXISTS idx_web_chat_updated ON web_chat_sessions(provider, updated_at);
	`
	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	fmt.Printf("Database initialized at %s\n", dbPath)
}

func SaveMessage(convID, msgID, role, content string) error {
	if convID == "" {
		return nil
	}
	query := `INSERT OR REPLACE INTO messages (conversation_id, msg_id, role, content) VALUES (?, ?, ?, ?)`
	_, err := DB.Exec(query, convID, msgID, role, content)
	return err
}

func SaveMessageIfMissing(convID, msgID, role, content string) error {
	if convID == "" {
		return nil
	}
	query := `INSERT OR IGNORE INTO messages (conversation_id, msg_id, role, content) VALUES (?, ?, ?, ?)`
	_, err := DB.Exec(query, convID, msgID, role, content)
	return err
}

func GetLocalHistory(convID string) ([]models.Message, error) {
	query := `SELECT role, content FROM messages WHERE conversation_id = ? ORDER BY id ASC`
	rows, err := DB.Query(query, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, nil
}

func StableMessageID(convID, role, content string, occurrence int) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s|%s|%s|%d", convID, role, content, occurrence)))
	return "sync_" + hex.EncodeToString(sum[:])
}

func SaveSession(fingerprint, convID string) error {
	query := `INSERT OR REPLACE INTO sessions (fingerprint, conversation_id) VALUES (?, ?)`
	_, err := DB.Exec(query, fingerprint, convID)
	return err
}

func GetSession(fingerprint string) (string, error) {
	var convID string
	query := `SELECT conversation_id FROM sessions WHERE fingerprint = ?`
	err := DB.QueryRow(query, fingerprint).Scan(&convID)
	if err == nil {
		return convID, nil
	}
	return "", err
}

func FindSessionByMessage(role, content string) (string, error) {
	var convID string
	// Try to find a conversation that starts with this message
	query := `SELECT conversation_id FROM messages WHERE role = ? AND content = ? ORDER BY created_at ASC LIMIT 1`
	err := DB.QueryRow(query, role, content).Scan(&convID)
	if err != nil {
		return "", err
	}
	return convID, nil
}

func SaveAgentState(id, goal, status, stateJson string) error {
	query := `INSERT OR REPLACE INTO agent_states (id, goal, status, state_json, updated_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`
	_, err := DB.Exec(query, id, goal, status, stateJson)
	return err
}

func GetAgentState(id string) (string, string, string, error) {
	var goal, status, stateJson string
	query := `SELECT goal, status, state_json FROM agent_states WHERE id = ?`
	err := DB.QueryRow(query, id).Scan(&goal, &status, &stateJson)
	return goal, status, stateJson, err
}

type WebChatState struct {
	Provider           string
	SessionKey         string
	ChatID             string
	ParentMessageID    string
	Model              string
	Title              string
	RolloverCount      int
	EstimatedTokens    int
	ClientMessageCount int
	LastMessageHash    string
}

func GetWebChatState(provider, sessionKey string) (WebChatState, error) {
	if DB == nil {
		return WebChatState{}, sql.ErrConnDone
	}
	var state WebChatState
	query := `SELECT provider, session_key, chat_id, parent_message_id, model, title,
		rollover_count, estimated_tokens, client_message_count, last_message_hash
		FROM web_chat_sessions WHERE provider = ? AND session_key = ?`
	err := DB.QueryRow(query, provider, sessionKey).Scan(
		&state.Provider,
		&state.SessionKey,
		&state.ChatID,
		&state.ParentMessageID,
		&state.Model,
		&state.Title,
		&state.RolloverCount,
		&state.EstimatedTokens,
		&state.ClientMessageCount,
		&state.LastMessageHash,
	)
	return state, err
}

func SaveWebChatState(state WebChatState) error {
	if DB == nil || state.Provider == "" || state.SessionKey == "" {
		return nil
	}
	query := `INSERT INTO web_chat_sessions (
			provider, session_key, chat_id, parent_message_id, model, title,
			rollover_count, estimated_tokens, client_message_count, last_message_hash, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(provider, session_key) DO UPDATE SET
			chat_id = excluded.chat_id,
			parent_message_id = excluded.parent_message_id,
			model = excluded.model,
			title = excluded.title,
			rollover_count = excluded.rollover_count,
			estimated_tokens = excluded.estimated_tokens,
			client_message_count = excluded.client_message_count,
			last_message_hash = excluded.last_message_hash,
			updated_at = CURRENT_TIMESTAMP`
	_, err := DB.Exec(query,
		state.Provider,
		state.SessionKey,
		state.ChatID,
		state.ParentMessageID,
		state.Model,
		state.Title,
		state.RolloverCount,
		state.EstimatedTokens,
		state.ClientMessageCount,
		state.LastMessageHash,
	)
	return err
}
