package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// MigrateLegacyStockPrompts appends an English prompt version only when the
// active template still equals a stock template from the previous release.
// Existing versions remain in history, and edited prompts are never replaced.
// Custom agents use the generic assistant starter only if that exact starter
// is still active. The reporter uses its own default.
func (d *DB) MigrateLegacyStockPrompts(defaults map[string]string) error {
	rows, err := d.Query(`SELECT id, key, builtin FROM agents WHERE current_prompt_id IS NOT NULL`)
	if err != nil {
		return err
	}
	type candidate struct {
		id      int64
		key     string
		builtin bool
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.key, &c.builtin); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, c := range candidates {
		defaultKey := c.key
		if !c.builtin && c.key != "reporter" {
			defaultKey = "_assistant"
		}
		newTemplate, hasDefault := defaults[defaultKey]
		oldHash, hasLegacy := legacyStockPromptHashes[defaultKey]
		if !hasDefault || !hasLegacy {
			continue
		}
		if err := d.migrateLegacyStockPrompt(c.id, oldHash, newTemplate); err != nil {
			return fmt.Errorf("migrate prompt for agent %s: %w", c.key, err)
		}
	}
	return nil
}

func (d *DB) migrateLegacyStockPrompt(agentID int64, oldHash, newTemplate string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRow(`SELECT p.template_text FROM agents a
JOIN agent_prompts p ON p.id=a.current_prompt_id
WHERE a.id=$1 FOR UPDATE OF a`, agentID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !matchesLegacyText(current, oldHash) || current == newTemplate {
		return nil
	}
	var version int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM agent_prompts WHERE agent_id=$1`, agentID).Scan(&version); err != nil {
		return err
	}
	var promptID int64
	if err := tx.QueryRow(`INSERT INTO agent_prompts(agent_id,version,template_text,note,updated_by)
VALUES ($1,$2,$3,$4,$5) RETURNING id`, agentID, version, newTemplate,
		"English built-in default", "system").Scan(&promptID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agents SET current_prompt_id=$1 WHERE id=$2`, promptID, agentID); err != nil {
		return err
	}
	return tx.Commit()
}
