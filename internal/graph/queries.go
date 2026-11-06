package graph

import (
	"context"
	"fmt"
	"regexp"
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

var cloudPattern = regexp.MustCompile(`(?i).*(github\.io|amazonaws\.com|herokuapp\.com|pantheon\.io|azurewebsites\.net|trafficmanager\.net|ghost\.io|wpengine\.com|zendesk\.com|fastly\.net|myshopify\.com|surge\.sh).*`)

// GetFullGraph returns nodes and edges for vis.js rendering.
// limit caps the node count (0 = default 200).
func GetFullGraph(ctx context.Context, c *Client, limit int) (*GraphResponse, error) {
	if limit <= 0 {
		limit = 200
	}

	nodeSet := make(map[string]GraphNode)
	var edges []GraphEdge
	seenEdges := make(map[string]bool)

	addNode := func(id, label, group string) bool {
		if _, exists := nodeSet[id]; exists {
			return true
		}
		if len(nodeSet) >= limit {
			return false
		}
		rawLabel := label
		if len(label) > 40 {
			label = label[:40] + "…"
		}
		nodeSet[id] = GraphNode{
			ID:    id,
			Label: label,
			Group: group,
			Title: group + ": " + rawLabel,
		}
		return true
	}

	// 1. Fetch domains up to initial limit (half limit to leave room for connected nodes)
	initLim := limit / 2
	if initLim < 1 {
		initLim = 1
	}

	domRows, err := c.DB().QueryContext(ctx, `SELECT name FROM domains LIMIT ?`, initLim)
	if err != nil {
		return nil, fmt.Errorf("query domains: %w", err)
	}
	defer domRows.Close()

	for domRows.Next() {
		var name string
		if err := domRows.Scan(&name); err == nil && name != "" {
			addNode("domain:"+name, name, "Domain")
		}
	}

	// Helper to add edge if both nodes are valid and within limit
	addEdge := func(fromID, toID, toLabel, toGroup, relLabel string) {
		if _, ok := nodeSet[fromID]; !ok {
			return
		}
		if !addNode(toID, toLabel, toGroup) {
			return
		}
		edgeKey := fromID + "->" + toID + ":" + relLabel
		if !seenEdges[edgeKey] {
			seenEdges[edgeKey] = true
			edges = append(edges, GraphEdge{From: fromID, To: toID, Label: relLabel})
		}
	}

	// 2. Fetch edges from junction tables
	// domain_ips -> RESOLVES_TO
	ipRows, err := c.DB().QueryContext(ctx, `
		SELECT di.domain, di.ip 
		FROM domain_ips di
		JOIN domains d ON d.name = di.domain
	`)
	if err == nil {
		defer ipRows.Close()
		for ipRows.Next() {
			var domain, ip string
			if err := ipRows.Scan(&domain, &ip); err == nil && domain != "" && ip != "" {
				addEdge("domain:"+domain, "ip:"+ip, ip, "IP", "RESOLVES_TO")
			}
		}
	}

	// domain_cnames -> HAS_CNAME
	cnameRows, err := c.DB().QueryContext(ctx, `
		SELECT dc.domain, dc.cname 
		FROM domain_cnames dc
		JOIN domains d ON d.name = dc.domain
	`)
	if err == nil {
		defer cnameRows.Close()
		for cnameRows.Next() {
			var domain, cname string
			if err := cnameRows.Scan(&domain, &cname); err == nil && domain != "" && cname != "" {
				addEdge("domain:"+domain, "cname:"+cname, cname, "CNAME", "HAS_CNAME")
			}
		}
	}

	// domain_technologies -> RUNS_ON
	techRows, err := c.DB().QueryContext(ctx, `
		SELECT dt.domain, dt.tech 
		FROM domain_technologies dt
		JOIN domains d ON d.name = dt.domain
	`)
	if err == nil {
		defer techRows.Close()
		for techRows.Next() {
			var domain, tech string
			if err := techRows.Scan(&domain, &tech); err == nil && domain != "" && tech != "" {
				addEdge("domain:"+domain, "tech:"+tech, tech, "Technology", "RUNS_ON")
			}
		}
	}

	// domain_titles -> HAS_TITLE
	titleRows, err := c.DB().QueryContext(ctx, `
		SELECT dt.domain, dt.title 
		FROM domain_titles dt
		JOIN domains d ON d.name = dt.domain
	`)
	if err == nil {
		defer titleRows.Close()
		for titleRows.Next() {
			var domain, title string
			if err := titleRows.Scan(&domain, &title); err == nil && domain != "" && title != "" {
				addEdge("domain:"+domain, "title:"+title, title, "HTTPTitle", "HAS_TITLE")
			}
		}
	}

	// 3. Backfill any additional domains up to limit
	if len(nodeSet) < limit {
		extraDomRows, err := c.DB().QueryContext(ctx, `SELECT name FROM domains LIMIT ?`, limit)
		if err == nil {
			defer extraDomRows.Close()
			for extraDomRows.Next() {
				var name string
				if err := extraDomRows.Scan(&name); err == nil && name != "" {
					if !addNode("domain:"+name, name, "Domain") {
						break
					}
				}
			}
		}
	}

	// 4. Backfill any standalone IPs, CNAMEs, Techs, Titles if still under limit
	if len(nodeSet) < limit {
		extraIPRows, err := c.DB().QueryContext(ctx, `SELECT address FROM ips LIMIT ?`, limit)
		if err == nil {
			defer extraIPRows.Close()
			for extraIPRows.Next() {
				var addr string
				if err := extraIPRows.Scan(&addr); err == nil && addr != "" {
					if !addNode("ip:"+addr, addr, "IP") {
						break
					}
				}
			}
		}
	}

	if len(nodeSet) == 0 {
		return &GraphResponse{Nodes: []GraphNode{}, Edges: []GraphEdge{}}, nil
	}

	return buildResponse(nodeSet, edges), nil
}

// QueryDanglingDNS finds domains with CNAMEs pointing to cloud providers that have no IP.
func QueryDanglingDNS(ctx context.Context, c *Client) (*GraphResponse, error) {
	query := `
		SELECT DISTINCT d.name, dc.cname
		FROM domain_cnames dc
		JOIN domains d ON d.name = dc.domain
		LEFT JOIN domain_ips di ON di.domain = d.name
		WHERE di.ip IS NULL
		  AND (
			dc.cname LIKE '%github.io%' OR
			dc.cname LIKE '%amazonaws.com%' OR
			dc.cname LIKE '%herokuapp.com%' OR
			dc.cname LIKE '%pantheon.io%' OR
			dc.cname LIKE '%azurewebsites.net%' OR
			dc.cname LIKE '%trafficmanager.net%' OR
			dc.cname LIKE '%ghost.io%' OR
			dc.cname LIKE '%wpengine.com%' OR
			dc.cname LIKE '%zendesk.com%' OR
			dc.cname LIKE '%fastly.net%' OR
			dc.cname LIKE '%myshopify.com%' OR
			dc.cname LIKE '%surge.sh%'
		  )
	`
	rows, err := c.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query dangling dns: %w", err)
	}
	defer rows.Close()

	var pairs [][2]string
	for rows.Next() {
		var domain, cname string
		if err := rows.Scan(&domain, &cname); err != nil {
			continue
		}
		if cloudPattern.MatchString(cname) {
			pairs = append(pairs, [2]string{domain, cname})
		}
	}

	return buildSimpleGraph(pairs, "DANGLING_CNAME", "domain:", "cname:", "Domain", "CNAME"), nil
}

// QuerySharedInfrastructure finds IPs serving multiple domains.
func QuerySharedInfrastructure(ctx context.Context, c *Client) (*GraphResponse, error) {
	query := `
		SELECT di1.domain AS domain1, di1.ip AS ip, di2.domain AS domain2
		FROM domain_ips di1
		JOIN domain_ips di2 ON di1.ip = di2.ip AND di1.domain < di2.domain
		JOIN ips i ON i.address = di1.ip
		ORDER BY di1.ip
	`
	rows, err := c.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query shared infra: %w", err)
	}
	defer rows.Close()

	nodeSet := make(map[string]GraphNode)
	var edges []GraphEdge
	seenEdges := make(map[string]bool)

	for rows.Next() {
		var d1, ip, d2 string
		if err := rows.Scan(&d1, &ip, &d2); err != nil {
			continue
		}

		dom1ID := "domain:" + d1
		dom2ID := "domain:" + d2
		ipID := "ip:" + ip

		if d1 != "" {
			nodeSet[dom1ID] = GraphNode{ID: dom1ID, Label: d1, Group: "Domain", Title: "Domain: " + d1}
		}
		if d2 != "" {
			nodeSet[dom2ID] = GraphNode{ID: dom2ID, Label: d2, Group: "Domain", Title: "Domain: " + d2}
		}
		if ip != "" {
			nodeSet[ipID] = GraphNode{ID: ipID, Label: ip, Group: "IP", Title: "IP: " + ip}
			if d1 != "" {
				edgeKey1 := dom1ID + "->" + ipID + ":RESOLVES_TO"
				if !seenEdges[edgeKey1] {
					seenEdges[edgeKey1] = true
					edges = append(edges, GraphEdge{From: dom1ID, To: ipID, Label: "RESOLVES_TO"})
				}
			}
			if d2 != "" {
				edgeKey2 := dom2ID + "->" + ipID + ":RESOLVES_TO"
				if !seenEdges[edgeKey2] {
					seenEdges[edgeKey2] = true
					edges = append(edges, GraphEdge{From: dom2ID, To: ipID, Label: "RESOLVES_TO"})
				}
			}
		}
	}

	return buildResponse(nodeSet, edges), nil
}

// QueryTechnologyExposure returns domains and the technologies they expose.
func QueryTechnologyExposure(ctx context.Context, c *Client) (*GraphResponse, error) {
	query := `
		SELECT DISTINCT dt.domain, dt.tech
		FROM domain_technologies dt
		ORDER BY dt.tech, dt.domain
	`
	rows, err := c.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query tech exposure: %w", err)
	}
	defer rows.Close()

	var pairs [][2]string
	for rows.Next() {
		var domain, tech string
		if err := rows.Scan(&domain, &tech); err != nil {
			continue
		}
		pairs = append(pairs, [2]string{domain, tech})
	}

	return buildSimpleGraph(pairs, "RUNS_ON", "domain:", "tech:", "Domain", "Technology"), nil
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

func buildSimpleGraph(pairs [][2]string, relLabel, fromPrefix, toPrefix, fromGroup, toGroup string) *GraphResponse {
	nodeSet := make(map[string]GraphNode)
	var edges []GraphEdge
	seenEdges := make(map[string]bool)

	for _, pair := range pairs {
		fromVal := pair[0]
		toVal := pair[1]
		if fromVal == "" || toVal == "" {
			continue
		}

		fromID := fromPrefix + fromVal
		toID := toPrefix + toVal

		fromLbl := fromVal
		if len(fromLbl) > 40 {
			fromLbl = fromLbl[:40] + "…"
		}
		toLbl := toVal
		if len(toLbl) > 40 {
			toLbl = toLbl[:40] + "…"
		}

		nodeSet[fromID] = GraphNode{
			ID:    fromID,
			Label: fromLbl,
			Group: fromGroup,
			Title: fromGroup + ": " + fromVal,
		}
		nodeSet[toID] = GraphNode{
			ID:    toID,
			Label: toLbl,
			Group: toGroup,
			Title: toGroup + ": " + toVal,
		}

		edgeKey := fromID + "->" + toID + ":" + relLabel
		if !seenEdges[edgeKey] {
			seenEdges[edgeKey] = true
			edges = append(edges, GraphEdge{From: fromID, To: toID, Label: relLabel})
		}
	}

	return buildResponse(nodeSet, edges)
}
