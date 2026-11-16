package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

const (
	resourceID = "team_space"
	wikiName   = "Вики команды Alpha"
	awaiting   = "awaiting_confirmation"
	pending    = "pending_approval"
)

var (
	errNotFound  = errors.New("draft not found")
	errStale     = errors.New("draft version changed")
	errCorporate = errors.New("corporate service unavailable")
)

type Draft struct {
	ID        string  `json:"draft_id"`
	URL       string  `json:"draft_url"`
	Resource  string  `json:"resource_name"`
	Reason    string  `json:"reason"`
	Status    string  `json:"status"`
	Version   int     `json:"version"`
	RequestID *string `json:"request_id"`
	NextStep  string  `json:"next_step"`
}

func (d *Draft) fillDetails() {
	d.Resource = wikiName
	d.URL = "/drafts/" + d.ID
	d.NextStep = "Проверь причину и подтверди отправку."
	if d.Status == pending {
		d.NextStep = "Дождись согласования наставником. Пока ждёшь, изучи вводную команды."
	}
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS buddy_drafts (
  id TEXT PRIMARY KEY,
  owner TEXT NOT NULL UNIQUE,
  reason TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('awaiting_confirmation', 'pending_approval')),
  version INTEGER NOT NULL CHECK (version > 0),
  request_id TEXT
);
CREATE TABLE IF NOT EXISTS buddy_corporate_requests (
  draft_id TEXT PRIMARY KEY REFERENCES buddy_drafts(id),
  request_id TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK (status = 'pending_approval')
);
`

func openStore(ctx context.Context, driver, dsn string) (*Store, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
	}
	if _, err = db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}

const selectDraft = `SELECT id, reason, status, version, request_id FROM buddy_drafts`

func scanDraft(row *sql.Row) (*Draft, error) {
	var d Draft
	err := row.Scan(&d.ID, &d.Reason, &d.Status, &d.Version, &d.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	d.fillDetails()
	return &d, nil
}

func (s *Store) current(ctx context.Context, owner string) (*Draft, error) {
	return scanDraft(s.db.QueryRowContext(ctx, selectDraft+` WHERE owner = $1`, owner))
}

func (s *Store) get(ctx context.Context, owner, id string) (*Draft, error) {
	return scanDraft(s.db.QueryRowContext(ctx, selectDraft+` WHERE owner = $1 AND id = $2`, owner, id))
}

func (s *Store) prepare(ctx context.Context, owner, reason string) (*Draft, error) {
	id := randomID()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO buddy_drafts (id, owner, reason, status, version)
		VALUES ($1, $2, $3, $4, 1) ON CONFLICT (owner) DO NOTHING`, id, owner, reason, awaiting)
	if err != nil {
		return nil, err
	}
	return s.current(ctx, owner)
}

type CorporateService interface {
	Create(context.Context, *sql.Tx, string) (requestID string, status string, err error)
}

type CorporateMock struct{}

func (CorporateMock) Create(ctx context.Context, tx *sql.Tx, draftID string) (string, string, error) {
	id := "REQ-" + draftID[:12]
	_, err := tx.ExecContext(ctx, `
		INSERT INTO buddy_corporate_requests (draft_id, request_id, status)
		VALUES ($1, $2, $3) ON CONFLICT (draft_id) DO NOTHING`, draftID, id, pending)
	if err != nil {
		return "", "", err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT request_id, status FROM buddy_corporate_requests WHERE draft_id = $1`, draftID).Scan(&id, &status)
	return id, status, err
}

func (s *Store) confirm(ctx context.Context, corp CorporateService, owner, id string, version int) (*Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// The write locks the selected draft before we decide whether to call the mock.
	draft, err := scanDraft(tx.QueryRowContext(ctx, `
		UPDATE buddy_drafts SET version = version WHERE owner = $1 AND id = $2
		RETURNING id, reason, status, version, request_id`, owner, id))
	if err != nil {
		return nil, err
	}
	if draft.Version != version {
		return nil, errStale
	}
	if draft.RequestID != nil {
		return draft, nil
	}
	number, status, err := corp.Create(ctx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCorporate, err)
	}
	if number == "" || status != pending {
		return nil, errCorporate
	}
	_, err = tx.ExecContext(ctx, `UPDATE buddy_drafts SET request_id = $1, status = $2 WHERE id = $3`, number, status, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	draft.Status = status
	draft.RequestID = &number
	draft.fillDetails()
	return draft, nil
}

func randomID() string {
	var bytes [16]byte
	rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:])
}
