package graph

import (
	"context"
	"fmt"
	"log"
	"strings"

	"infragraph/internal/parser"
)

// IngestSubfinder loads Subfinder results into the graph.
// Creates Domain nodes and RESOLVES_TO relationships to IP nodes.
func IngestSubfinder(ctx context.Context, c *Client, results []parser.SubfinderResult) error {
	for _, r := range results {
		if r.Host == "" {
			continue
		}
		// Merge domain node
		if err := c.RunWrite(ctx,
			`MERGE (d:Domain {name: $name}) SET d.source = coalesce(d.source, $source)`,
			map[string]any{"name": strings.ToLower(r.Host), "source": r.Source},
		); err != nil {
			log.Printf("[warn] subfinder domain merge %s: %v", r.Host, err)
		}

		// If an IP was resolved, link it
		if r.IP != "" {
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (i:IP {address: $ip})
				 MERGE (d)-[:RESOLVES_TO]->(i)`,
				map[string]any{"domain": strings.ToLower(r.Host), "ip": r.IP},
			); err != nil {
				log.Printf("[warn] subfinder ip link %s->%s: %v", r.Host, r.IP, err)
			}
		}
	}
	return nil
}

// IngestAmass loads Amass results into the graph.
// Creates Domain, IP, and CNAME nodes with their relationships.
func IngestAmass(ctx context.Context, c *Client, results []parser.AmassResult) error {
	for _, r := range results {
		if r.Name == "" {
			continue
		}
		domain := strings.ToLower(r.Name)

		// Merge domain node
		if err := c.RunWrite(ctx,
			`MERGE (d:Domain {name: $name}) SET d.source = coalesce(d.source, $source), d.tag = coalesce(d.tag, $tag)`,
			map[string]any{"name": domain, "source": r.Source, "tag": r.Tag},
		); err != nil {
			log.Printf("[warn] amass domain merge %s: %v", domain, err)
			continue
		}

		// Merge IP nodes and RESOLVES_TO relationships
		for _, addr := range r.Addresses {
			if addr.IP == "" {
				continue
			}
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (i:IP {address: $ip})
				 SET i.cidr = coalesce(i.cidr, $cidr), i.asn = coalesce(i.asn, $asn), i.desc = coalesce(i.desc, $desc)
				 MERGE (d)-[:RESOLVES_TO]->(i)`,
				map[string]any{
					"domain": domain,
					"ip":     addr.IP,
					"cidr":   addr.CIDR,
					"asn":    fmt.Sprintf("%d", addr.ASN),
					"desc":   addr.Desc,
				},
			); err != nil {
				log.Printf("[warn] amass ip link %s->%s: %v", domain, addr.IP, err)
			}
		}
	}
	return nil
}

// IngestHttpx loads httpx probe results into the graph.
// Creates HTTP titles, technologies, CNAME relationships.
func IngestHttpx(ctx context.Context, c *Client, results []parser.HttpxResult) error {
	for _, r := range results {
		host := r.Input
		if host == "" {
			host = r.Host
		}
		if host == "" {
			continue
		}
		host = strings.ToLower(strings.TrimSpace(host))

		// Ensure domain node exists
		if err := c.RunWrite(ctx,
			`MERGE (d:Domain {name: $name})
			 SET d.status_code = $status, d.url = $url, d.scheme = $scheme`,
			map[string]any{
				"name":   host,
				"status": r.StatusCode,
				"url":    r.URL,
				"scheme": r.Scheme,
			},
		); err != nil {
			log.Printf("[warn] httpx domain merge %s: %v", host, err)
			continue
		}

		// Merge HTTPTitle node
		if r.Title != "" {
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (t:HTTPTitle {text: $title})
				 MERGE (d)-[:HAS_TITLE]->(t)`,
				map[string]any{"domain": host, "title": r.Title},
			); err != nil {
				log.Printf("[warn] httpx title merge %s: %v", host, err)
			}
		}

		// Merge IP nodes if available
		ips := r.A
		if len(ips) == 0 && r.HostIP != "" {
			ips = []string{r.HostIP}
		}
		for _, ip := range ips {
			if ip == "" {
				continue
			}
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (i:IP {address: $ip})
				 MERGE (d)-[:RESOLVES_TO]->(i)`,
				map[string]any{"domain": host, "ip": ip},
			); err != nil {
				log.Printf("[warn] httpx ip link %s->%s: %v", host, ip, err)
			}
		}

		// Merge Technology nodes
		for _, tech := range r.Tech {
			if tech == "" {
				continue
			}
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (t:Technology {name: $tech})
				 MERGE (d)-[:RUNS_ON]->(t)`,
				map[string]any{"domain": host, "tech": tech},
			); err != nil {
				log.Printf("[warn] httpx tech merge %s->%s: %v", host, tech, err)
			}
		}

		// Merge CNAME nodes
		for _, cname := range r.CNAME {
			if cname == "" {
				continue
			}
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (c:CNAME {target: $cname})
				 MERGE (d)-[:HAS_CNAME]->(c)`,
				map[string]any{"domain": host, "cname": strings.ToLower(cname)},
			); err != nil {
				log.Printf("[warn] httpx cname merge %s->%s: %v", host, cname, err)
			}
		}

		// Web server as technology
		if r.WebServer != "" {
			if err := c.RunWrite(ctx,
				`MERGE (d:Domain {name: $domain})
				 MERGE (t:Technology {name: $tech})
				 MERGE (d)-[:RUNS_ON]->(t)`,
				map[string]any{"domain": host, "tech": "WebServer:" + r.WebServer},
			); err != nil {
				log.Printf("[warn] httpx webserver merge: %v", err)
			}
		}
	}
	return nil
}
