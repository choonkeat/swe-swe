package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// handleList lists all initialized swe-swe projects and auto-prunes missing ones
func handleList() {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	prune := fs.Bool("prune", false, "Remove orphaned project directories")
	fs.Parse(os.Args[2:])

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Failed to get home directory: %v", err)
	}

	projectsDir := filepath.Join(homeDir, ".swe-swe", "projects")

	// Check if projects directory exists
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No projects initialized yet")
			return
		}
		log.Fatalf("Failed to read projects directory: %v", err)
	}

	type projectInfo struct {
		path      string
		config    InitConfig
		hasConfig bool
	}
	var activeProjects []projectInfo
	var prunedCount int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		metadataDir := filepath.Join(projectsDir, entry.Name())
		pathFile := filepath.Join(metadataDir, ".path")

		// Read the .path file
		pathData, err := os.ReadFile(pathFile)
		if err != nil {
			// If .path file doesn't exist or can't be read, handle based on prune flag
			if os.IsNotExist(err) {
				if *prune {
					if err := os.RemoveAll(metadataDir); err != nil {
						fmt.Printf("Warning: failed to remove orphaned %s: %v\n", entry.Name(), err)
					} else {
						prunedCount++
					}
				} else {
					fmt.Printf("Warning: .path file missing in %s (use --prune to remove)\n", entry.Name())
				}
			}
			continue
		}

		projectPath := string(pathData)

		// Check if the original path still exists
		if _, err := os.Stat(projectPath); os.IsNotExist(err) {
			// Project path no longer exists, remove metadata directory
			if err := os.RemoveAll(metadataDir); err != nil {
				fmt.Printf("Warning: Failed to remove stale metadata directory %s: %v\n", metadataDir, err)
			}
			prunedCount++
		} else {
			// Project path exists, try to load init config
			info := projectInfo{path: projectPath}
			if cfg, err := loadInitConfig(metadataDir); err == nil {
				info.config = cfg
				info.hasConfig = true
			}
			activeProjects = append(activeProjects, info)
		}
	}

	// Display active projects
	if len(activeProjects) == 0 {
		fmt.Println("No projects initialized yet")
	} else {
		fmt.Printf("Initialized projects (%d):\n", len(activeProjects))
		for _, info := range activeProjects {
			if info.hasConfig {
				agents := strings.Join(info.config.Agents, ",")
				extras := []string{}
				if info.config.AptPackages != "" {
					extras = append(extras, "apt:"+info.config.AptPackages)
				}
				if info.config.NpmPackages != "" {
					extras = append(extras, "npm:"+info.config.NpmPackages)
				}
				if info.config.WithDocker {
					extras = append(extras, "docker")
				}
				if len(info.config.SlashCommands) > 0 {
					extras = append(extras, fmt.Sprintf("slash-cmds:%d", len(info.config.SlashCommands)))
				}
				if info.config.SSL != "" && info.config.SSL != "no" {
					extras = append(extras, "ssl:"+info.config.SSL)
				}
				if len(extras) > 0 {
					fmt.Printf("  %s [%s] (%s)\n", info.path, agents, strings.Join(extras, ", "))
				} else {
					fmt.Printf("  %s [%s]\n", info.path, agents)
				}
			} else {
				fmt.Printf("  %s\n", info.path)
			}
		}
	}

	// Show pruning summary if any projects were removed
	if prunedCount > 0 {
		fmt.Printf("\nRemoved %d stale project(s)\n", prunedCount)
	}
}
