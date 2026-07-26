package runner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

// Run orchestrates all recon tools against the target, collecting JSON output files.
func Run(cfg Config) (*Result, error) {
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

// runSubfinder executes subfinder and writes JSONL output to outPath.
func runSubfinder(target, outPath, timeout string) error {
	sf, err := exec.LookPath("subfinder")
	if err != nil {
		// Try GOPATH/bin
		home, _ := os.UserHomeDir()
		sf = filepath.Join(home, "go", "bin", "subfinder")
		if _, err2 := os.Stat(sf); err2 != nil {
			return fmt.Errorf("subfinder not found in PATH or ~/go/bin")
		}
	}
	
	args := []string{
		"-d", target,
		"-json",
		"-o", outPath,
	}
	
	if timeout != "" {
		args = append(args, "-max-time", timeout)
	}

	cmd := exec.Command(sf, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runAmass executes amass enum and writes JSONL output to outPath.
func runAmass(target, outPath, timeout string) error {
	amass, err := exec.LookPath("amass-v4")
	if err != nil {
		home, _ := os.UserHomeDir()
		amass = filepath.Join(home, "go", "bin", "amass-v4")
		if _, err2 := os.Stat(amass); err2 != nil {
			return fmt.Errorf("amass v4 not found in PATH or ~/go/bin")
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

// runHttpx probes a list of domains and writes JSONL to outPath.
func runHttpx(domainsFile, outPath string) error {
	// Resolution order:
	// 1. ~/go/bin/httpx  (local Go install — avoids Python httpx at /usr/bin/httpx)
	// 2. "httpx" in PATH (works in Docker where it's in /usr/local/bin)
	// 3. "httpx-toolkit"  (Kali/Debian package name)
	home, _ := os.UserHomeDir()
	httpx := filepath.Join(home, "go", "bin", "httpx")
	if _, err := os.Stat(httpx); err != nil {
		// Try PATH — but verify it's ProjectDiscovery's httpx, not the Python one
		httpx, err = exec.LookPath("httpx")
		if err != nil {
			httpx, err = exec.LookPath("httpx-toolkit")
			if err != nil {
				return fmt.Errorf("ProjectDiscovery httpx not found at ~/go/bin/httpx, in PATH, or as httpx-toolkit")
			}
		}
	}

	cmd := exec.Command(httpx,
		"-l", domainsFile,
		"-json",
		"-o", outPath,
		"-title",
		"-tech-detect",
		"-ip",
		"-cname", // Crucial: fetches CNAMEs now that we don't get them from Amass JSON
		"-silent",
		"-follow-redirects",
		"-status-code",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
