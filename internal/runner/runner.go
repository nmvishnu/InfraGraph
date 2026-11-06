package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/projectdiscovery/goflags"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/gologger/levels"
	httpxRunner "github.com/projectdiscovery/httpx/runner"
	"github.com/projectdiscovery/subfinder/v2/pkg/resolve"
	sfRunner "github.com/projectdiscovery/subfinder/v2/pkg/runner"
)

// Config holds the runner configuration for a target scan.
type Config struct {
	Target    string
	OutputDir string
	Timeout   string // e.g. "30m"
}

// Result holds output file paths after all tools complete.
type Result struct {
	SubfinderFile string
	AmassFile     string
	HttpxFile     string
	DomainsFile   string // merged unique domain list
}

// subfinderRecord mirrors the JSONL structure expected by parser.ParseSubfinder.
type subfinderRecord struct {
	Host   string `json:"host"`
	Source string `json:"source"`
	IP     string `json:"ip"`
}

// httpxOutputRecord mirrors the JSONL structure expected by parser.ParseHttpx.
type httpxOutputRecord struct {
	Input      string   `json:"input"`
	Host       string   `json:"host"`
	URL        string   `json:"url"`
	Scheme     string   `json:"scheme"`
	StatusCode int      `json:"status_code"`
	Title      string   `json:"title"`
	Tech       []string `json:"tech,omitempty"`
	A          []string `json:"a,omitempty"`
	CNAME      []string `json:"cname,omitempty"`
	WebServer  string   `json:"webserver,omitempty"`
}

// Run orchestrates all recon tools against the target, collecting JSON output files.
func Run(cfg Config) (*Result, error) {
	// Silence verbose library logs (Subfinder, httpx, etc.) globally once at start
	gologger.DefaultLogger.SetMaxLevel(levels.LevelSilent)

	// Wipe the output directory to prevent old domain files from bleeding
	// into the current scan (especially if a tool fails or times out).
	if err := os.RemoveAll(cfg.OutputDir); err != nil {
		log.Printf("[runner] Warning: failed to wipe output directory: %v", err)
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	result := &Result{
		SubfinderFile: filepath.Join(cfg.OutputDir, "subfinder.json"),
		AmassFile:     filepath.Join(cfg.OutputDir, "amass.json"),
		DomainsFile:   filepath.Join(cfg.OutputDir, "domains.txt"),
		HttpxFile:     filepath.Join(cfg.OutputDir, "httpx.json"),
	}

	// Phase 1: Run Subfinder and Amass concurrently
	var wg sync.WaitGroup
	var subfinderErr, amassErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[runner] Starting Subfinder on %s ...", cfg.Target)
		subfinderErr = runSubfinder(cfg.Target, result.SubfinderFile, cfg.Timeout)
		if subfinderErr != nil {
			log.Printf("[runner] Subfinder error: %v", subfinderErr)
		} else {
			log.Printf("[runner] Subfinder complete → %s", result.SubfinderFile)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("[runner] Starting Amass on %s ...", cfg.Target)
		amassErr = runAmass(cfg.Target, result.AmassFile, cfg.Timeout)
		if amassErr != nil {
			log.Printf("[runner] Amass error: %v", amassErr)
		} else {
			log.Printf("[runner] Amass complete → %s", result.AmassFile)
		}
	}()

	wg.Wait()

	// At least one tool must succeed
	if subfinderErr != nil && amassErr != nil {
		return nil, fmt.Errorf("both subfinder and amass failed: subfinder=%v amass=%v", subfinderErr, amassErr)
	}

	// Phase 2: Merge discovered domains into a flat list for httpx
	if err := mergeDomains(cfg.Target, result.SubfinderFile, result.AmassFile, result.DomainsFile); err != nil {
		return nil, fmt.Errorf("merge domains: %w", err)
	}

	// Phase 3: Run httpx against the merged domain list
	log.Printf("[runner] Starting httpx probe ...")
	if err := runHttpx(result.DomainsFile, result.HttpxFile); err != nil {
		log.Printf("[runner] httpx error: %v", err)
	} else {
		log.Printf("[runner] httpx complete → %s", result.HttpxFile)
	}

	return result, nil
}

// parseTimeoutMinutes parses timeout strings like "30m", "30", or durations into integer minutes.
func parseTimeoutMinutes(timeoutStr string, defaultMinutes int) int {
	if timeoutStr == "" {
		return defaultMinutes
	}
	if d, err := time.ParseDuration(timeoutStr); err == nil {
		m := int(d.Minutes())
		if m > 0 {
			return m
		}
	}
	trimmed := strings.TrimSuffix(strings.TrimSpace(timeoutStr), "m")
	if n, err := strconv.Atoi(trimmed); err == nil && n > 0 {
		return n
	}
	return defaultMinutes
}

// runSubfinder enumerates subdomains using subfinder as an embedded library and writes JSONL output to outPath.
func runSubfinder(target, outPath, timeout string) error {
	// Always ensure outPath exists so downstream parsers and mergeDomains don't fail
	if err := os.WriteFile(outPath, []byte(""), 0644); err != nil {
		log.Printf("[runner] Warning: failed to initialize %s: %v", outPath, err)
	}

	timeoutMinutes := parseTimeoutMinutes(timeout, 30)

	var (
		mu      sync.Mutex
		results []subfinderRecord
	)

	opts := &sfRunner.Options{
		Threads:            10,
		Timeout:            30,
		MaxEnumerationTime: timeoutMinutes,
		ResultCallback: func(result *resolve.HostEntry) {
			if result == nil || result.Host == "" {
				return
			}
			mu.Lock()
			results = append(results, subfinderRecord{
				Host:   result.Host,
				Source: result.Source,
				IP:     "",
			})
			mu.Unlock()
		},
	}

	sf, err := sfRunner.NewRunner(opts)
	if err != nil {
		return fmt.Errorf("create subfinder runner: %w", err)
	}

	ctx := context.Background()
	if _, err := sf.EnumerateSingleDomainWithCtx(ctx, target, nil); err != nil {
		return fmt.Errorf("subfinder enumeration failed: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create subfinder output file: %w", err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, r := range results {
		data, err := json.Marshal(r)
		if err != nil {
			continue
		}
		if _, err := writer.Write(data); err != nil {
			return fmt.Errorf("write subfinder record: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}
	return writer.Flush()
}

// runAmass executes amass enum if available and writes output to outPath.
// Amass is optional — if amass-v4 is not found, it skips gracefully without error.
func runAmass(target, outPath, timeout string) error {
	// Always ensure outPath exists so downstream parsers and mergeDomains don't fail
	if err := os.WriteFile(outPath, []byte(""), 0644); err != nil {
		log.Printf("[runner] Warning: failed to initialize %s: %v", outPath, err)
	}

	amass, err := exec.LookPath("amass-v4")
	if err != nil {
		home, _ := os.UserHomeDir()
		amass = filepath.Join(home, "go", "bin", "amass-v4")
		if _, err2 := os.Stat(amass); err2 != nil {
			log.Printf("[runner] amass-v4 not found — skipping (subfinder provides sufficient coverage)")
			return nil
		}
	}
	args := []string{
		"enum",
		"-d", target,
		"-o", outPath, // Amass v4+ dropped -json, so we use plain text output
		"-passive",
	}
	if timeout != "" {
		args = append(args, "-timeout", timeout)
	}
	cmd := exec.Command(amass, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runHttpx probes discovered domains using httpx as an embedded library and writes JSONL output to outPath.
func runHttpx(domainsFile, outPath string) error {
	// Always ensure outPath exists so downstream parsers don't fail
	if err := os.WriteFile(outPath, []byte(""), 0644); err != nil {
		log.Printf("[runner] Warning: failed to initialize %s: %v", outPath, err)
	}

	data, err := os.ReadFile(domainsFile)
	if err != nil {
		return fmt.Errorf("read domains file %s: %w", domainsFile, err)
	}

	var targets []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			targets = append(targets, line)
		}
	}

	if len(targets) == 0 {
		log.Printf("[runner] No domains to probe with httpx")
		return nil
	}

	var (
		mu      sync.Mutex
		results []httpxOutputRecord
	)

	options := httpxRunner.Options{
		Methods:         "GET",
		InputTargetHost: goflags.StringSlice(targets),
		ExtractTitle:    true,
		TechDetect:      true,
		StatusCode:      true,
		FollowRedirects: true,
		Threads:         25,
		Timeout:         10,
		OnResult: func(r httpxRunner.Result) {
			if r.Err != nil {
				return
			}
			host := r.Host
			if host == "" {
				host = r.Input
			}
			scheme := r.Scheme
			if scheme == "" {
				if strings.HasPrefix(r.URL, "https://") {
					scheme = "https"
				} else if strings.HasPrefix(r.URL, "http://") {
					scheme = "http"
				}
			}

			ips := r.A
			if len(ips) == 0 && r.HostIP != "" {
				ips = []string{r.HostIP}
			}

			record := httpxOutputRecord{
				Input:      r.Input,
				Host:       host,
				URL:        r.URL,
				Scheme:     scheme,
				StatusCode: r.StatusCode,
				Title:      r.Title,
				Tech:       r.Technologies,
				A:          ips,
				CNAME:      r.CNAMEs,
				WebServer:  r.WebServer,
			}

			mu.Lock()
			results = append(results, record)
			mu.Unlock()
		},
	}

	if err := options.ValidateOptions(); err != nil {
		return fmt.Errorf("validate httpx options: %w", err)
	}

	hpRunner, err := httpxRunner.New(&options)
	if err != nil {
		return fmt.Errorf("create httpx runner: %w", err)
	}
	defer hpRunner.Close()

	hpRunner.RunEnumeration()

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create httpx output file: %w", err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, r := range results {
		line, err := json.Marshal(r)
		if err != nil {
			continue
		}
		if _, err := writer.Write(line); err != nil {
			return fmt.Errorf("write httpx record: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}
	return writer.Flush()
}

// mergeDomains reads subfinder and amass JSON files, extracts hostnames,
// deduplicates them (while strictly enforcing scope), and writes a flat text file for httpx.
func mergeDomains(target, subfinderPath, amassPath, outPath string) error {
	seen := make(map[string]struct{})
	target = strings.ToLower(target)

	// Ensure the domain is strictly the target or a subdomain of it
	inScope := func(domain string) bool {
		domain = strings.ToLower(domain)
		return domain == target || strings.HasSuffix(domain, "."+target)
	}

	addFromFile := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Extract "host" field quickly without full JSON parse (Subfinder)
			if idx := strings.Index(line, `"host":"`); idx != -1 {
				rest := line[idx+8:]
				end := strings.Index(rest, `"`)
				if end > 0 {
					domain := strings.ToLower(rest[:end])
					if inScope(domain) {
						seen[domain] = struct{}{}
					}
				}
				continue
			}
			// For old amass JSON: "name" field
			if idx := strings.Index(line, `"name":"`); idx != -1 {
				rest := line[idx+8:]
				end := strings.Index(rest, `"`)
				if end > 0 {
					domain := strings.ToLower(rest[:end])
					if inScope(domain) {
						seen[domain] = struct{}{}
					}
				}
				continue
			}
			// If it's not JSON, it might be Amass v4 text output (e.g., "example.com (FQDN) --> ...")
			if !strings.Contains(line, "{") && strings.Contains(line, ".") {
				parts := strings.Split(line, " ")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					// Look for parts that look like domains and aren't relationship markers
					if strings.Contains(p, ".") && !strings.Contains(p, "-->") && !strings.HasPrefix(p, "(") {
						domain := strings.ToLower(p)
						if inScope(domain) {
							seen[domain] = struct{}{}
						}
					}
				}
			}
		}
	}

	addFromFile(subfinderPath)
	addFromFile(amassPath)

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for domain := range seen {
		fmt.Fprintln(f, domain)
	}
	log.Printf("[runner] Merged %d unique domains → %s", len(seen), outPath)
	return nil
}
