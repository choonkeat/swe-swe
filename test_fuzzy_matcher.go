package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// This is a simple test to verify the fuzzy matcher implementation
func main() {
	// Get current working directory
	workingDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("Testing fuzzy matcher in directory: %s\n", workingDir)
	
	// Create fuzzy matcher instance
	fm := NewFuzzyMatcher(workingDir)
	
	// Index files
	fmt.Println("Indexing files...")
	if err := fm.IndexFiles(); err != nil {
		log.Fatalf("Failed to index files: %v", err)
	}
	
	fmt.Printf("Indexed %d files\n", fm.GetFileCount())
	
	// Test some queries
	testQueries := []string{
		"main",
		"elm",
		"css",
		"go",
		"makefile",
		"README",
		"statstylcss", // Example from the specification
		"fuzzy",
	}
	
	for _, query := range testQueries {
		fmt.Printf("\nSearching for '%s':\n", query)
		results := fm.Search(query, 5)
		
		if len(results) == 0 {
			fmt.Println("  No results found")
			continue
		}
		
		for i, result := range results {
			fmt.Printf("  %d. %s (score: %d)\n", i+1, result.File.RelPath, result.Score)
			if result.File.IsDir {
				fmt.Printf("     [Directory]\n")
			}
		}
	}
}