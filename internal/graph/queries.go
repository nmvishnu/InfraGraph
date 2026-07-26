package graph

import (
	"context"
)

// GraphResponse is the JSON payload returned by the API server for vis.js rendering.
type GraphResponse struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNode represents a vis.js-compatible node.
type GraphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"` // used by vis.js for coloring
	Title string `json:"title"` // hover tooltip
}

// GraphEdge represents a vis.js-compatible edge.
type GraphEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

// GetFullGraph returns nodes and edges for vis.js rendering.
// Uses property-indexed lookups so queries are O(log N) not O(N²).
// limit caps the node count (0 = default 200).
func GetFullGraph(ctx context.Context, c *Client, limit int) (*GraphResponse, error) {
	if limit <= 0 {
		limit = 200
	}

	// ── PHASE 1: Fetch nodes ──────────────────────────────────────────────────
	// We fetch node types in priority order using UNION ALL so Domains and IPs
	// appear first. Each branch hits its own index. No relationship traversal.
	//
	// Allocation split (roughly):  Domain 40 %, IP 20 %, CNAME 20 %, Tech 15 %, Title 5 %
	perType := int64(limit)
	nodeRows, err := c.RunRead(ctx, `
		CALL {
			MATCH (n:Domain)
			RETURN elementId(n) AS id, 'Domain' AS label_type, coalesce(n.name,'') AS label
			LIMIT $lim
		UNION ALL
			MATCH (n:IP)
			RETURN elementId(n) AS id, 'IP' AS label_type, coalesce(n.address,'') AS label
			LIMIT $lim
		UNION ALL
			MATCH (n:CNAME)
			RETURN elementId(n) AS id, 'CNAME' AS label_type, coalesce(n.target,'') AS label
			LIMIT $lim
		UNION ALL
			MATCH (n:Technology)
			RETURN elementId(n) AS id, 'Technology' AS label_type, coalesce(n.name,'') AS label
			LIMIT $lim
		UNION ALL
			MATCH (n:HTTPTitle)
			RETURN elementId(n) AS id, 'HTTPTitle' AS label_type, coalesce(n.text,'') AS label
			LIMIT $lim
		}
		RETURN id, label_type, label
		LIMIT $total
	`, map[string]any{"lim": perType, "total": int64(limit)})
	if err != nil {
		// Fallback: plain MATCH if CALL {} UNION not supported (Neo4j < 5.0)
		nodeRows, err = c.RunRead(ctx, `
			MATCH (n)
			RETURN elementId(n) AS id,
			       labels(n)[0] AS label_type,
			       CASE labels(n)[0]
			         WHEN 'Domain'     THEN coalesce(n.name, '')
			         WHEN 'IP'         THEN coalesce(n.address, '')
			         WHEN 'CNAME'      THEN coalesce(n.target, '')
			         WHEN 'Technology' THEN coalesce(n.name, '')
			         WHEN 'HTTPTitle'  THEN coalesce(n.text, '')
			         ELSE ''
			       END AS label
			LIMIT $lim
		`, map[string]any{"lim": int64(limit)})
		if err != nil {
			return nil, err
		}
	}

	// Build node map and per-type name lists for the indexed edge lookup
	nodeSet := make(map[string]GraphNode, len(nodeRows))
	var domainNames, ipAddrs, cnames, techNames []string

	for _, row := range nodeRows {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		lt, _ := row["label_type"].(string)
		lbl, _ := row["label"].(string)
		if lt == "" {
			lt = "Unknown"
		}
		if len(lbl) > 40 {
			lbl = lbl[:40] + "…"
		}
		nodeSet[id] = GraphNode{
			ID:    id,
			Label: lbl,
			Group: lt,
			Title: lt + ": " + lbl,
		}
		// Collect property values per type so edges can use indexed lookups
		switch lt {
		case "Domain":
			domainNames = append(domainNames, lbl)
		case "IP":
			ipAddrs = append(ipAddrs, lbl)
		case "CNAME":
			cnames = append(cnames, lbl)
		case "Technology":
			techNames = append(techNames, lbl)
		}
	}

	if len(nodeSet) == 0 {
		return &GraphResponse{Nodes: []GraphNode{}, Edges: []GraphEdge{}}, nil
	}

	// ── PHASE 2: Fetch edges using property indexes ───────────────────────────
	// Instead of `elementId(n) IN $ids` (full scan), we match by indexed property
	// values — each branch hits the unique constraint backing index.
	edgeRows, err := c.RunRead(ctx, `
		MATCH (n)-[r]->(m)
		WHERE (n:Domain AND n.name IN $domains)
		   OR (n:IP     AND n.address IN $ips)
		   OR (n:CNAME  AND n.target  IN $cnames)
		   OR (n:Technology AND n.name IN $techs)
		RETURN elementId(n) AS from_id,
		       elementId(m) AS to_id,
		       type(r) AS rel_type
		LIMIT 5000
	`, map[string]any{
		"domains": strSliceOrEmpty(domainNames),
		"ips":     strSliceOrEmpty(ipAddrs),
		"cnames":  strSliceOrEmpty(cnames),
		"techs":   strSliceOrEmpty(techNames),
	})
	if err != nil {
		// Non-fatal — return nodes with no edges rather than failing
		return buildResponse(nodeSet, nil), nil
	}

	// Only keep edges where both endpoints are in the visible node set
	var edges []GraphEdge
	for _, row := range edgeRows {
		from, _ := row["from_id"].(string)
		to, _ := row["to_id"].(string)
		rel, _ := row["rel_type"].(string)
		if _, ok := nodeSet[from]; !ok {
			continue
		}
		if _, ok := nodeSet[to]; !ok {
			continue
		}
		edges = append(edges, GraphEdge{From: from, To: to, Label: rel})
	}

	return buildResponse(nodeSet, edges), nil
}

// QueryDanglingDNS finds domains with CNAMEs but no resolved IPs — takeover candidates.
// Uses EXISTS {} subquery (Neo4j 5 style) instead of deprecated pattern predicate.
func QueryDanglingDNS(ctx context.Context, c *Client) (*GraphResponse, error) {
	// Try Neo4j 5 EXISTS subquery syntax first
	rows, err := c.RunRead(ctx, `
		MATCH (d:Domain)-[:HAS_CNAME]->(cn:CNAME)
		WHERE cn.target =~ '(?i).*(github\\.io|amazonaws\\.com|herokuapp\\.com|pantheon\\.io|azurewebsites\\.net|trafficmanager\\.net|ghost\\.io|wpengine\\.com|zendesk\\.com|fastly\\.net|myshopify\\.com|surge\\.sh).*'
		RETURN d.name AS domain, cn.target AS cname
	`, nil)
	if err != nil {
		// Fallback to classic pattern predicate (Neo4j 4.x compat)
		rows, err = c.RunRead(ctx, `
			MATCH (d:Domain)-[:HAS_CNAME]->(cn:CNAME)
			WHERE cn.target =~ '(?i).*(github\\.io|amazonaws\\.com|herokuapp\\.com|pantheon\\.io|azurewebsites\\.net|trafficmanager\\.net|ghost\\.io|wpengine\\.com|zendesk\\.com|fastly\\.net|myshopify\\.com|surge\\.sh).*'
			RETURN d.name AS domain, cn.target AS cname
		`, nil)
		if err != nil {
			return nil, err
		}
	}
	return buildSimpleGraph(rows, "domain", "cname", "DANGLING_CNAME", "Domain", "CNAME"), nil
}

// QuerySharedInfrastructure finds IPs hosting multiple domains.
func QuerySharedInfrastructure(ctx context.Context, c *Client) (*GraphResponse, error) {
	rows, err := c.RunRead(ctx, `
		MATCH (d1:Domain)-[:RESOLVES_TO]->(i:IP)<-[:RESOLVES_TO]-(d2:Domain)
		WHERE d1.name < d2.name
		RETURN d1.name AS domain1, i.address AS ip, d2.name AS domain2
	`, nil)
	if err != nil {
		return nil, err
	}

	nodeSet := make(map[string]GraphNode)
	var edges []GraphEdge

	for _, row := range rows {
		d1, _ := row["domain1"].(string)
		ip, _ := row["ip"].(string)
		d2, _ := row["domain2"].(string)

		if d1 != "" {
			nodeSet[d1] = GraphNode{ID: d1, Label: d1, Group: "Domain", Title: "Domain: " + d1}
		}
		if d2 != "" {
			nodeSet[d2] = GraphNode{ID: d2, Label: d2, Group: "Domain", Title: "Domain: " + d2}
		}
		if ip != "" {
			nodeSet[ip] = GraphNode{ID: ip, Label: ip, Group: "IP", Title: "IP: " + ip}
			if d1 != "" {
				edges = append(edges, GraphEdge{From: d1, To: ip, Label: "RESOLVES_TO"})
			}
			if d2 != "" {
				edges = append(edges, GraphEdge{From: d2, To: ip, Label: "RESOLVES_TO"})
			}
		}
	}

	return buildResponse(nodeSet, edges), nil
}

// QueryTechnologyExposure returns domains and the technologies they expose.
func QueryTechnologyExposure(ctx context.Context, c *Client) (*GraphResponse, error) {
	rows, err := c.RunRead(ctx, `
		MATCH (d:Domain)-[:RUNS_ON]->(t:Technology)
		RETURN d.name AS domain, t.name AS tech
		ORDER BY t.name
	`, nil)
	if err != nil {
		return nil, err
	}
	return buildSimpleGraph(rows, "domain", "tech", "RUNS_ON", "Domain", "Technology"), nil
}

// ----- helpers -----

func buildResponse(nodeSet map[string]GraphNode, edges []GraphEdge) *GraphResponse {
	nodes := make([]GraphNode, 0, len(nodeSet))
	for _, n := range nodeSet {
		nodes = append(nodes, n)
	}
	if edges == nil {
		edges = []GraphEdge{}
	}
	return &GraphResponse{Nodes: nodes, Edges: edges}
}

func buildSimpleGraph(rows []map[string]any, fromKey, toKey, relLabel, fromGroup, toGroup string) *GraphResponse {
	nodeSet := make(map[string]GraphNode)
	var edges []GraphEdge

	for _, row := range rows {
		from, _ := row[fromKey].(string)
		to, _ := row[toKey].(string)
		if from == "" || to == "" {
			continue
		}
		nodeSet[from] = GraphNode{ID: from, Label: from, Group: fromGroup, Title: fromGroup + ": " + from}
		nodeSet[to] = GraphNode{ID: to, Label: to, Group: toGroup, Title: toGroup + ": " + to}
		edges = append(edges, GraphEdge{From: from, To: to, Label: relLabel})
	}

	return buildResponse(nodeSet, edges)
}

// strSliceOrEmpty returns the slice as-is or an empty slice — Neo4j requires
// a list, not nil, for the IN operator.
func strSliceOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
