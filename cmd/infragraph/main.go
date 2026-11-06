package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"infragraph/internal/api"
	"infragraph/internal/graph"
	"infragraph/internal/parser"
	"infragraph/internal/runner"
)

const banner = `
╔══════════════════════════════════════════════════════╗
║  InfraGraph — Attack Surface Graph Correlator        ║
║  OSINT / ASM Pipeline  |  Red Team / Bug Bounty      ║
╚══════════════════════════════════════════════════════╝
`

func main() {
	fmt.Print(banner)

	// ----- CLI flags -----
	target     := flag.String("target", "", "Target domain to enumerate (e.g. example.com)")
	outputDir  := flag.String("output", "output", "Directory for raw JSON outputs")
	neoURI     := flag.String("neo4j", "bolt://localhost:7687", "Neo4j bolt URI")
	neoUser    := flag.String("user", "neo4j", "Neo4j username")
	neoPass    := flag.String("pass", "infragraph", "Neo4j password")
	listenAddr := flag.String("listen", ":8080", "API/UI server address")
	webRoot    := flag.String("web", "web", "Static web root directory")
	ingestOnly := flag.Bool("ingest-only", false, "Skip recon, only ingest existing JSON files in -output dir")
	serveOnly  := flag.Bool("serve-only", false, "Skip recon and ingestion, only serve the UI")
	timeout    := flag.String("timeout", "30", "Amass timeout in minutes")

	// ---- New flags ----
	fresh := flag.Bool("fresh", false,
		"Delete all nodes for -target before re-ingesting (keeps other targets intact)")
	reset := flag.Bool("reset", false,
		"Wipe the ENTIRE graph database before ingesting — use with care")

	flag.Parse()

	// Validate
	if !*serveOnly && !*ingestOnly && *target == "" {
		fmt.Fprintln(os.Stderr, "Error: -target is required (e.g. -target example.com)")
		fmt.Fprintln(os.Stderr, "Usage: infragraph -target <domain> [flags]")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if *fresh && *target == "" {
		fmt.Fprintln(os.Stderr, "Error: -fresh requires -target to know which domain to clear")
		os.Exit(1)
	}
	if *fresh && *reset {
		fmt.Fprintln(os.Stderr, "Error: -fresh and -reset are mutually exclusive")
		os.Exit(1)
	}

	ctx := context.Background()

	// ----- Connect to Neo4j -----
	log.Printf("[main] Connecting to Neo4j at %s ...", *neoURI)
	client, err := graph.NewClient(*neoURI, *neoUser, *neoPass)
	if err != nil {
		log.Fatalf("[main] Neo4j connection error: %v", err)
	}
	defer client.Close(ctx)

	// Retry connectivity up to 10s (helpful if Neo4j is still starting)
	for i := 0; i < 5; i++ {
		if err := client.Verify(ctx); err == nil {
			break
		} else if i == 4 {
			log.Fatalf("[main] Cannot reach Neo4j after retries: %v\nMake sure Neo4j is running on %s", err, *neoURI)
		}
		log.Printf("[main] Waiting for Neo4j (%d/5)...", i+1)
		time.Sleep(2 * time.Second)
	}
	log.Printf("[main] Neo4j connected ✓")

	// ----- Reset / Fresh -----
	if *reset {
		log.Printf("[main] -reset: wiping entire graph database...")
		if err := client.ClearAll(ctx); err != nil {
			log.Fatalf("[main] Reset failed: %v", err)
		}
		log.Printf("[main] Graph wiped ✓")
	} else if *fresh {
		log.Printf("[main] -fresh: clearing existing nodes for %s ...", *target)
		if err := client.ClearTarget(ctx, *target); err != nil {
			log.Fatalf("[main] Fresh clear failed: %v", err)
		}
		log.Printf("[main] Domain %s cleared ✓", *target)
	}

	// Ensure schema constraints
	if err := client.EnsureConstraints(ctx); err != nil {
		log.Printf("[main] Constraint setup warning: %v", err)
	}

	// ----- Recon Phase -----
	var runResult *runner.Result
	if !*serveOnly {
		if !*ingestOnly {
			log.Printf("[main] Starting recon pipeline against: %s", *target)
			runResult, err = runner.Run(runner.Config{
				Target:    *target,
				OutputDir: *outputDir,
				Timeout:   *timeout,
			})
			if err != nil {
				log.Fatalf("[main] Recon pipeline failed: %v", err)
			}
		} else {
			// Use existing files
			runResult = &runner.Result{
				SubfinderFile: filepath.Join(*outputDir, "subfinder.json"),
				AmassFile:     filepath.Join(*outputDir, "amass.json"),
				HttpxFile:     filepath.Join(*outputDir, "httpx.json"),
			}
			log.Printf("[main] Ingest-only mode: reading from %s", *outputDir)
		}

		// ----- Ingest Phase -----
		log.Printf("[main] Ingesting Subfinder results...")
		sfResults, err := parser.ParseSubfinder(runResult.SubfinderFile)
		if err != nil {
			log.Printf("[main] Subfinder parse warning: %v", err)
		} else {
			if err := graph.IngestSubfinder(ctx, client, sfResults); err != nil {
				log.Printf("[main] Subfinder ingest warning: %v", err)
			}
			log.Printf("[main] Ingested %d Subfinder records", len(sfResults))
		}

		log.Printf("[main] Ingesting Amass results...")
		amassResults, err := parser.ParseAmass(runResult.AmassFile)
		if err != nil {
			log.Printf("[main] Amass parse warning: %v", err)
		} else {
			if err := graph.IngestAmass(ctx, client, amassResults); err != nil {
				log.Printf("[main] Amass ingest warning: %v", err)
			}
			log.Printf("[main] Ingested %d Amass records", len(amassResults))
		}

		log.Printf("[main] Ingesting httpx results...")
		httpxResults, err := parser.ParseHttpx(runResult.HttpxFile)
		if err != nil {
			log.Printf("[main] httpx parse warning: %v", err)
		} else {
			if err := graph.IngestHttpx(ctx, client, httpxResults); err != nil {
				log.Printf("[main] httpx ingest warning: %v", err)
			}
			log.Printf("[main] Ingested %d httpx records", len(httpxResults))
		}

		log.Printf("[main] Ingestion complete ✓")
	}

	// ----- Serve Phase -----
	log.Printf("[main] Starting UI server on http://localhost%s", *listenAddr)
	srv := api.NewServer(client, *webRoot)
	if err := srv.Start(*listenAddr); err != nil {
		log.Fatalf("[main] Server error: %v", err)
	}
}
