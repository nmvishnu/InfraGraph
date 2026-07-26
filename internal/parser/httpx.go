package parser

import (
	"bufio"
	"encoding/json"
	"os"
)

// HttpxResult mirrors the JSONL structure output by httpx -json
type HttpxResult struct {
	URL        string   `json:"url"`
	Input      string   `json:"input"`
	Title      string   `json:"title"`
	Host       string   `json:"host"`
	HostIP     string   `json:"host_ip"`
	A          []string `json:"a"`
	StatusCode int      `json:"status_code"`
	Tech       []string `json:"tech"`
	WebServer  string   `json:"webserver"`
	CDN        string   `json:"cdn_name"`
	CNAME      []string `json:"cname"`
	Port       string   `json:"port"`
	Scheme     string   `json:"scheme"`
	BodyHash   string   `json:"body_sha256"`
	Words      int      `json:"words"`
	Lines      int      `json:"lines"`
}

// ParseHttpx reads an httpx JSONL output file and returns all results.
func ParseHttpx(path string) ([]HttpxResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []HttpxResult
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r HttpxResult
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, scanner.Err()
}
