package parser

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// AmassAddress holds an IP address with network metadata from an Amass result
type AmassAddress struct {
	IP   string `json:"ip"`
	CIDR string `json:"cidr"`
	ASN  int    `json:"asn"`
	Desc string `json:"desc"`
}

// AmassResult mirrors the JSONL structure output by Amass enum -json
type AmassResult struct {
	Name      string         `json:"name"`
	Domain    string         `json:"domain"`
	Addresses []AmassAddress `json:"addresses"`
	Tag       string         `json:"tag"`
	Source    string         `json:"source"`
}

// ParseAmass reads an Amass JSONL output file and returns all results.
func ParseAmass(path string) ([]AmassResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []AmassResult
	scanner := bufio.NewScanner(f)
	// Amass can output large lines; increase buffer
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r AmassResult
		if err := json.Unmarshal(line, &r); err != nil {
			// Fallback: parse Amass v4 plain text or graph output
			text := string(line)
			if !strings.Contains(text, "{") && strings.Contains(text, ".") {
				parts := strings.Split(text, " ")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if strings.Contains(p, ".") && !strings.Contains(p, "-->") && !strings.HasPrefix(p, "(") {
						r = AmassResult{
							Name:   p,
							Source: "amass",
						}
						results = append(results, r)
					}
				}
			}
			continue
		}
		results = append(results, r)
	}
	return results, scanner.Err()
}
