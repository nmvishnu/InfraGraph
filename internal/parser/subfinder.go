package parser

import (
	"bufio"
	"encoding/json"
	"os"
)

// SubfinderResult mirrors the JSONL structure output by Subfinder
type SubfinderResult struct {
	Host   string `json:"host"`
	Source string `json:"source"`
	IP     string `json:"ip"`
}

// ParseSubfinder reads a Subfinder JSONL output file and returns all results.
func ParseSubfinder(path string) ([]SubfinderResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []SubfinderResult
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r SubfinderResult
		if err := json.Unmarshal(line, &r); err != nil {
			continue // skip malformed lines
		}
		results = append(results, r)
	}
	return results, scanner.Err()
}
