package graph

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Client wraps an embedded SQLite database connection.
type Client struct {
	db *sql.DB
}

// NewClient opens a SQLite database in WAL mode with busy timeout configured
// and sets MaxOpenConns to 1 for safe concurrent access.
func NewClient(dbPath string) (*Client, error) {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	dsn := "file:" + dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}

	db.SetMaxOpenConns(1)

	return &Client{db: db}, nil
}

// Close closes the underlying database.
func (c *Client) Close(ctx context.Context) {
	if c.db != nil {
		_ = c.db.Close()
	}
}

// Verify verifies database connectivity by pinging the database.
func (c *Client) Verify(ctx context.Context) error {
	if c.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return c.db.PingContext(ctx)
}

// DB returns the raw *sql.DB instance for direct queries.
func (c *Client) DB() *sql.DB {
	return c.db
}

// EnsureConstraints initializes all required tables and indexes.
func (c *Client) EnsureConstraints(ctx context.Context) error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS domains (
			name TEXT PRIMARY KEY,
			url TEXT,
			status_code INTEGER,
			scheme TEXT,
			source TEXT,
			tag TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS ips (
			address TEXT PRIMARY KEY,
			asn TEXT,
			cidr TEXT,
			desc TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS cnames (
			target TEXT PRIMARY KEY
		);`,
		`CREATE TABLE IF NOT EXISTS technologies (
			name TEXT PRIMARY KEY
		);`,
		`CREATE TABLE IF NOT EXISTS http_titles (
			text TEXT PRIMARY KEY
		);`,
		`CREATE TABLE IF NOT EXISTS domain_ips (
			domain TEXT,
			ip TEXT,
			PRIMARY KEY (domain, ip)
		);`,
		`CREATE TABLE IF NOT EXISTS domain_cnames (
			domain TEXT,
			cname TEXT,
			PRIMARY KEY (domain, cname)
		);`,
		`CREATE TABLE IF NOT EXISTS domain_technologies (
			domain TEXT,
			tech TEXT,
			PRIMARY KEY (domain, tech)
		);`,
		`CREATE TABLE IF NOT EXISTS domain_titles (
			domain TEXT,
			title TEXT,
			PRIMARY KEY (domain, title)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_domain_ips_ip ON domain_ips(ip);`,
		`CREATE INDEX IF NOT EXISTS idx_domain_cnames_cname ON domain_cnames(cname);`,
		`CREATE INDEX IF NOT EXISTS idx_domain_technologies_tech ON domain_technologies(tech);`,
		`CREATE INDEX IF NOT EXISTS idx_domain_titles_title ON domain_titles(title);`,
	}

	for _, stmt := range schema {
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure schema (%s): %w", stmt, err)
		}
	}

	return nil
}

// ClearTarget deletes domain and all its junction entries where domain = target OR domain LIKE '%.target'.
func (c *Client) ClearTarget(ctx context.Context, target string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("clear target tx begin: %w", err)
	}
	defer tx.Rollback()

	pattern := "%." + target
	stmts := []string{
		`DELETE FROM domain_ips WHERE domain = ? OR domain LIKE ?`,
		`DELETE FROM domain_cnames WHERE domain = ? OR domain LIKE ?`,
		`DELETE FROM domain_technologies WHERE domain = ? OR domain LIKE ?`,
		`DELETE FROM domain_titles WHERE domain = ? OR domain LIKE ?`,
		`DELETE FROM domains WHERE name = ? OR name LIKE ?`,
	}

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt, target, pattern); err != nil {
			return fmt.Errorf("clear target exec: %w", err)
		}
	}

	return tx.Commit()
}

// ClearAll deletes all data from all tables.
func (c *Client) ClearAll(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("clear all tx begin: %w", err)
	}
	defer tx.Rollback()

	tables := []string{
		"domain_ips",
		"domain_cnames",
		"domain_technologies",
		"domain_titles",
		"domains",
		"ips",
		"cnames",
		"technologies",
		"http_titles",
	}

	for _, tbl := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+tbl); err != nil {
			return fmt.Errorf("clear all exec (%s): %w", tbl, err)
		}
	}

	return tx.Commit()
}

// RunRead provides backward compatibility for legacy queries (such as /api/stats).
func (c *Client) RunRead(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(query, "labels(n)") {
		var results []map[string]any
		tables := []struct {
			label string
			table string
		}{
			{"Domain", "domains"},
			{"IP", "ips"},
			{"CNAME", "cnames"},
			{"Technology", "technologies"},
		}
		for _, t := range tables {
			var cnt int64
			_ = c.db.QueryRowContext(ctx, "SELECT count(*) FROM "+t.table).Scan(&cnt)
			results = append(results, map[string]any{
				"label": t.label,
				"cnt":   cnt,
			})
		}
		return results, nil
	}

	if strings.Contains(query, "MATCH ()-[r]->()") {
		var cnt1, cnt2, cnt3, cnt4 int64
		_ = c.db.QueryRowContext(ctx, "SELECT count(*) FROM domain_ips").Scan(&cnt1)
		_ = c.db.QueryRowContext(ctx, "SELECT count(*) FROM domain_cnames").Scan(&cnt2)
		_ = c.db.QueryRowContext(ctx, "SELECT count(*) FROM domain_technologies").Scan(&cnt3)
		_ = c.db.QueryRowContext(ctx, "SELECT count(*) FROM domain_titles").Scan(&cnt4)
		total := cnt1 + cnt2 + cnt3 + cnt4
		return []map[string]any{
			{"cnt": total},
		}, nil
	}

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var records []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any)
		for i, col := range cols {
			row[col] = vals[i]
		}
		records = append(records, row)
	}

	return records, rows.Err()
}
