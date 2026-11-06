package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Client wraps a Neo4j driver and provides session management.
type Client struct {
	driver neo4j.DriverWithContext
	uri    string
}

// NewClient creates a new authenticated Neo4j client.
func NewClient(uri, username, password string) (*Client, error) {
	driver, err := neo4j.NewDriverWithContext(
		uri,
		neo4j.BasicAuth(username, password, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("neo4j driver init: %w", err)
	}
	return &Client{driver: driver, uri: uri}, nil
}

// Verify pings the database to confirm connectivity.
func (c *Client) Verify(ctx context.Context) error {
	return c.driver.VerifyConnectivity(ctx)
}

// Close releases the driver connection pool.
func (c *Client) Close(ctx context.Context) {
	_ = c.driver.Close(ctx)
}

// Session returns a new Neo4j session. Caller must close it.
func (c *Client) Session(ctx context.Context) neo4j.SessionWithContext {
	return c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
}

// RunWrite executes a write Cypher query with parameters.
func (c *Client) RunWrite(ctx context.Context, cypher string, params map[string]any) error {
	session := c.Session(ctx)
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, cypher, params)
		return nil, err
	})
	return err
}

// RunRead executes a read Cypher query and returns raw records.
func (c *Client) RunRead(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := c.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}
		var rows []map[string]any
		for res.Next(ctx) {
			record := res.Record()
			row := make(map[string]any)
			for _, key := range record.Keys {
				row[key] = record.AsMap()[key]
			}
			rows = append(rows, row)
		}
		return rows, res.Err()
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.([]map[string]any), nil
}

// EnsureConstraints creates uniqueness constraints for idempotent ingestion.
func (c *Client) EnsureConstraints(ctx context.Context) error {
	constraints := []string{
		`CREATE CONSTRAINT domain_name_unique IF NOT EXISTS FOR (d:Domain) REQUIRE d.name IS UNIQUE`,
		`CREATE CONSTRAINT ip_address_unique IF NOT EXISTS FOR (i:IP) REQUIRE i.address IS UNIQUE`,
		`CREATE CONSTRAINT cname_target_unique IF NOT EXISTS FOR (c:CNAME) REQUIRE c.target IS UNIQUE`,
		`CREATE CONSTRAINT tech_name_unique IF NOT EXISTS FOR (t:Technology) REQUIRE t.name IS UNIQUE`,
	}
	for _, q := range constraints {
		if err := c.RunWrite(ctx, q, nil); err != nil {
			return fmt.Errorf("constraint setup: %w", err)
		}
	}
	return nil
}

// ClearTarget deletes all nodes (and their relationships) associated with a
// given target domain. Useful for a -fresh scan that re-ingests clean data
// without wiping other targets stored in the same database.
func (c *Client) ClearTarget(ctx context.Context, target string) error {
	// Catches exact match (example.com) and all subdomains (*.example.com)
	return c.RunWrite(ctx, `
		MATCH (d:Domain)
		WHERE d.name = $exact OR d.name ENDS WITH $suffix
		DETACH DELETE d
	`, map[string]any{"exact": target, "suffix": "." + target})
}

// ClearAll wipes the entire graph — all nodes and relationships.
func (c *Client) ClearAll(ctx context.Context) error {
	// Delete in batches of 5000 to avoid heap exhaustion on large graphs
	for {
		rows, err := c.RunRead(ctx, `MATCH (n) RETURN count(n) AS cnt`, nil)
		if err != nil {
			return err
		}
		var cnt int64
		if len(rows) > 0 {
			cnt, _ = rows[0]["cnt"].(int64)
		}
		if cnt == 0 {
			break
		}
		if err := c.RunWrite(ctx, `MATCH (n) WITH n LIMIT 5000 DETACH DELETE n`, nil); err != nil {
			return err
		}
	}
	return nil
}
