package graph

import (
	"context"
	"fmt"
	"log"
	"strings"

	"infragraph/internal/parser"
)

// IngestSubfinder loads Subfinder results into SQLite.
// Inserts Domain records, IP records, and domain_ips junction entries.
func IngestSubfinder(ctx context.Context, c *Client, results []parser.SubfinderResult) error {
	tx, err := c.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("subfinder begin tx: %w", err)
	}
	defer tx.Rollback()

	stmtDomainInsert, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domains (name, source) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain insert: %w", err)
	}
	defer stmtDomainInsert.Close()

	stmtDomainUpdate, err := tx.PrepareContext(ctx, `
		UPDATE domains 
		SET source = ? 
		WHERE name = ? AND (source IS NULL OR source = '')
	`)
	if err != nil {
		return fmt.Errorf("prepare domain update: %w", err)
	}
	defer stmtDomainUpdate.Close()

	stmtIP, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO ips (address) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare ip insert: %w", err)
	}
	defer stmtIP.Close()

	stmtDomainIP, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domain_ips (domain, ip) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain_ip insert: %w", err)
	}
	defer stmtDomainIP.Close()

	for _, r := range results {
		if strings.TrimSpace(r.Host) == "" {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(r.Host))
		source := strings.TrimSpace(r.Source)

		if _, err := stmtDomainInsert.ExecContext(ctx, host, source); err != nil {
			log.Printf("[warn] subfinder domain insert %s: %v", host, err)
			continue
		}
		if source != "" {
			if _, err := stmtDomainUpdate.ExecContext(ctx, source, host); err != nil {
				log.Printf("[warn] subfinder domain update %s: %v", host, err)
			}
		}

		ip := strings.TrimSpace(r.IP)
		if ip != "" {
			if _, err := stmtIP.ExecContext(ctx, ip); err != nil {
				log.Printf("[warn] subfinder ip insert %s: %v", ip, err)
				continue
			}
			if _, err := stmtDomainIP.ExecContext(ctx, host, ip); err != nil {
				log.Printf("[warn] subfinder domain_ip %s->%s: %v", host, ip, err)
			}
		}
	}

	return tx.Commit()
}

// IngestAmass loads Amass results into SQLite.
// Inserts Domain, IP records, network metadata, and domain_ips junction entries.
func IngestAmass(ctx context.Context, c *Client, results []parser.AmassResult) error {
	tx, err := c.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("amass begin tx: %w", err)
	}
	defer tx.Rollback()

	stmtDomainInsert, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domains (name, source, tag) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain insert: %w", err)
	}
	defer stmtDomainInsert.Close()

	stmtDomainUpdate, err := tx.PrepareContext(ctx, `
		UPDATE domains SET
			source = CASE WHEN source IS NULL OR source = '' THEN ? ELSE source END,
			tag = CASE WHEN tag IS NULL OR tag = '' THEN ? ELSE tag END
		WHERE name = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare domain update: %w", err)
	}
	defer stmtDomainUpdate.Close()

	stmtIPInsert, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO ips (address, asn, cidr, desc) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare ip insert: %w", err)
	}
	defer stmtIPInsert.Close()

	stmtIPUpdate, err := tx.PrepareContext(ctx, `
		UPDATE ips SET
			asn = CASE WHEN asn IS NULL OR asn = '' THEN ? ELSE asn END,
			cidr = CASE WHEN cidr IS NULL OR cidr = '' THEN ? ELSE cidr END,
			desc = CASE WHEN desc IS NULL OR desc = '' THEN ? ELSE desc END
		WHERE address = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare ip update: %w", err)
	}
	defer stmtIPUpdate.Close()

	stmtDomainIP, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domain_ips (domain, ip) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain_ip insert: %w", err)
	}
	defer stmtDomainIP.Close()

	for _, r := range results {
		if strings.TrimSpace(r.Name) == "" {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(r.Name))
		source := strings.TrimSpace(r.Source)
		tag := strings.TrimSpace(r.Tag)

		if _, err := stmtDomainInsert.ExecContext(ctx, domain, source, tag); err != nil {
			log.Printf("[warn] amass domain insert %s: %v", domain, err)
			continue
		}
		if source != "" || tag != "" {
			if _, err := stmtDomainUpdate.ExecContext(ctx, source, tag, domain); err != nil {
				log.Printf("[warn] amass domain update %s: %v", domain, err)
			}
		}

		for _, addr := range r.Addresses {
			ip := strings.TrimSpace(addr.IP)
			if ip == "" {
				continue
			}
			var asnStr string
			if addr.ASN != 0 {
				asnStr = fmt.Sprintf("%d", addr.ASN)
			}
			cidr := strings.TrimSpace(addr.CIDR)
			desc := strings.TrimSpace(addr.Desc)

			if _, err := stmtIPInsert.ExecContext(ctx, ip, asnStr, cidr, desc); err != nil {
				log.Printf("[warn] amass ip insert %s: %v", ip, err)
				continue
			}
			if asnStr != "" || cidr != "" || desc != "" {
				if _, err := stmtIPUpdate.ExecContext(ctx, asnStr, cidr, desc, ip); err != nil {
					log.Printf("[warn] amass ip update %s: %v", ip, err)
				}
			}
			if _, err := stmtDomainIP.ExecContext(ctx, domain, ip); err != nil {
				log.Printf("[warn] amass domain_ip %s->%s: %v", domain, ip, err)
			}
		}
	}

	return tx.Commit()
}

// IngestHttpx loads httpx probe results into SQLite.
// Updates domain HTTP attributes, inserts HTTP titles, technologies, and CNAME relationships.
func IngestHttpx(ctx context.Context, c *Client, results []parser.HttpxResult) error {
	tx, err := c.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("httpx begin tx: %w", err)
	}
	defer tx.Rollback()

	stmtDomainInsert, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domains (name, url, status_code, scheme) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain insert: %w", err)
	}
	defer stmtDomainInsert.Close()

	stmtDomainUpdate, err := tx.PrepareContext(ctx, `
		UPDATE domains SET
			url = ?,
			status_code = ?,
			scheme = ?
		WHERE name = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare domain update: %w", err)
	}
	defer stmtDomainUpdate.Close()

	stmtTitle, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO http_titles (text) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare title insert: %w", err)
	}
	defer stmtTitle.Close()

	stmtDomainTitle, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domain_titles (domain, title) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain_title insert: %w", err)
	}
	defer stmtDomainTitle.Close()

	stmtIP, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO ips (address) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare ip insert: %w", err)
	}
	defer stmtIP.Close()

	stmtDomainIP, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domain_ips (domain, ip) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain_ip insert: %w", err)
	}
	defer stmtDomainIP.Close()

	stmtTech, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO technologies (name) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare tech insert: %w", err)
	}
	defer stmtTech.Close()

	stmtDomainTech, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domain_technologies (domain, tech) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain_tech insert: %w", err)
	}
	defer stmtDomainTech.Close()

	stmtCname, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO cnames (target) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare cname insert: %w", err)
	}
	defer stmtCname.Close()

	stmtDomainCname, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO domain_cnames (domain, cname) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare domain_cname insert: %w", err)
	}
	defer stmtDomainCname.Close()

	for _, r := range results {
		host := r.Input
		if host == "" {
			host = r.Host
		}
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}

		if _, err := stmtDomainInsert.ExecContext(ctx, host, r.URL, r.StatusCode, r.Scheme); err != nil {
			log.Printf("[warn] httpx domain insert %s: %v", host, err)
			continue
		}
		if _, err := stmtDomainUpdate.ExecContext(ctx, r.URL, r.StatusCode, r.Scheme, host); err != nil {
			log.Printf("[warn] httpx domain update %s: %v", host, err)
		}

		if title := strings.TrimSpace(r.Title); title != "" {
			if _, err := stmtTitle.ExecContext(ctx, title); err != nil {
				log.Printf("[warn] httpx title insert %s: %v", host, err)
			} else {
				if _, err := stmtDomainTitle.ExecContext(ctx, host, title); err != nil {
					log.Printf("[warn] httpx domain_title %s: %v", host, err)
				}
			}
		}

		ips := r.A
		if len(ips) == 0 && r.HostIP != "" {
			ips = []string{r.HostIP}
		}
		for _, rawIP := range ips {
			ip := strings.TrimSpace(rawIP)
			if ip == "" {
				continue
			}
			if _, err := stmtIP.ExecContext(ctx, ip); err != nil {
				log.Printf("[warn] httpx ip insert %s: %v", ip, err)
				continue
			}
			if _, err := stmtDomainIP.ExecContext(ctx, host, ip); err != nil {
				log.Printf("[warn] httpx domain_ip %s->%s: %v", host, ip, err)
			}
		}

		for _, rawTech := range r.Tech {
			tech := strings.TrimSpace(rawTech)
			if tech == "" {
				continue
			}
			if _, err := stmtTech.ExecContext(ctx, tech); err != nil {
				log.Printf("[warn] httpx tech insert %s: %v", tech, err)
				continue
			}
			if _, err := stmtDomainTech.ExecContext(ctx, host, tech); err != nil {
				log.Printf("[warn] httpx domain_tech %s->%s: %v", host, tech, err)
			}
		}

		for _, rawCname := range r.CNAME {
			cname := strings.ToLower(strings.TrimSpace(rawCname))
			if cname == "" {
				continue
			}
			if _, err := stmtCname.ExecContext(ctx, cname); err != nil {
				log.Printf("[warn] httpx cname insert %s: %v", cname, err)
				continue
			}
			if _, err := stmtDomainCname.ExecContext(ctx, host, cname); err != nil {
				log.Printf("[warn] httpx domain_cname %s->%s: %v", host, cname, err)
			}
		}

		if ws := strings.TrimSpace(r.WebServer); ws != "" {
			tech := "WebServer:" + ws
			if _, err := stmtTech.ExecContext(ctx, tech); err != nil {
				log.Printf("[warn] httpx webserver tech insert %s: %v", tech, err)
			} else {
				if _, err := stmtDomainTech.ExecContext(ctx, host, tech); err != nil {
					log.Printf("[warn] httpx webserver domain_tech %s->%s: %v", host, tech, err)
				}
			}
		}
	}

	return tx.Commit()
}
