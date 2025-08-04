package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileInfo represents a file in the index
type FileInfo struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	IsDir    bool      `json:"isDir"`
	ModTime  time.Time `json:"modTime"`
	RelPath  string    `json:"relPath"`
}

// MatchResult represents a fuzzy match result
type MatchResult struct {
	File      FileInfo `json:"file"`
	Score     int      `json:"score"`
	Matches   []int    `json:"matches"` // Character positions that matched
}

// FuzzyMatcher handles file indexing and fuzzy matching
type FuzzyMatcher struct {
	files      []FileInfo
	rootDir    string
	gitignore  map[string]bool
	exclusions []string
	mu         sync.RWMutex
	lastUpdate time.Time
}

// NewFuzzyMatcher creates a new fuzzy matcher instance
func NewFuzzyMatcher(rootDir string) *FuzzyMatcher {
	fm := &FuzzyMatcher{
		rootDir: rootDir,
		exclusions: []string{
			"node_modules",
			".git",
			"dist",
			"build",
			"out",
			"target",
			"bin",
			".elm-stuff",
			"elm-stuff",
			".next",
			".nuxt",
			"coverage",
			".nyc_output",
			"*.log",
			"*.tmp",
			"*.swp",
			"*.swo",
			"*~",
			".DS_Store",
			"Thumbs.db",
		},
		gitignore: make(map[string]bool),
	}
	
	return fm
}

// loadGitignore loads .gitignore patterns
func (fm *FuzzyMatcher) loadGitignore() error {
	gitignorePath := filepath.Join(fm.rootDir, ".gitignore")
	file, err := os.Open(gitignorePath)
	if err != nil {
		// .gitignore doesn't exist, that's okay
		return nil
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			fm.gitignore[line] = true
		}
	}
	
	return scanner.Err()
}

// shouldExclude checks if a path should be excluded
func (fm *FuzzyMatcher) shouldExclude(path, name string, isDir bool) bool {
	// Check exclusions
	for _, exclusion := range fm.exclusions {
		if strings.HasPrefix(exclusion, "*.") {
			// File extension pattern
			ext := exclusion[1:]
			if strings.HasSuffix(name, ext) {
				return true
			}
		} else if name == exclusion || (isDir && strings.Contains(path, "/"+exclusion+"/")) {
			return true
		}
	}
	
	// Check gitignore patterns (simplified)
	for pattern := range fm.gitignore {
		if strings.Contains(path, pattern) {
			return true
		}
	}
	
	// Exclude hidden files/directories (except .gitignore, .env files)
	if strings.HasPrefix(name, ".") && name != ".gitignore" && !strings.HasPrefix(name, ".env") {
		return true
	}
	
	return false
}

// IndexFiles builds the file index
func (fm *FuzzyMatcher) IndexFiles() error {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	
	// Load .gitignore patterns
	if err := fm.loadGitignore(); err != nil {
		return fmt.Errorf("failed to load .gitignore: %w", err)
	}
	
	files := []FileInfo{}
	
	err := filepath.WalkDir(fm.rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files that can't be accessed
		}
		
		// Get relative path
		relPath, err := filepath.Rel(fm.rootDir, path)
		if err != nil {
			return nil
		}
		
		// Skip root directory
		if relPath == "." {
			return nil
		}
		
		name := d.Name()
		isDir := d.IsDir()
		
		// Check if should be excluded
		if fm.shouldExclude(relPath, name, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		
		// Get file info
		info, err := d.Info()
		if err != nil {
			return nil // Skip if can't get info
		}
		
		files = append(files, FileInfo{
			Path:    path,
			Name:    name,
			IsDir:   isDir,
			ModTime: info.ModTime(),
			RelPath: relPath,
		})
		
		return nil
	})
	
	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}
	
	fm.files = files
	fm.lastUpdate = time.Now()
	
	return nil
}

// calculateFuzzyScore calculates a fuzzy match score
func calculateFuzzyScore(pattern, target string) (int, []int) {
	pattern = strings.ToLower(pattern)
	target = strings.ToLower(target)
	
	if pattern == "" {
		return 0, nil
	}
	
	if pattern == target {
		return 1000, nil // Exact match gets highest score
	}
	
	matches := []int{}
	score := 0
	patternIdx := 0
	targetIdx := 0
	
	// Find character matches
	for patternIdx < len(pattern) && targetIdx < len(target) {
		if pattern[patternIdx] == target[targetIdx] {
			matches = append(matches, targetIdx)
			score += 10
			
			// Bonus for consecutive matches
			if len(matches) > 1 && matches[len(matches)-1] == matches[len(matches)-2]+1 {
				score += 5
			}
			
			// Bonus for start of word matches
			if targetIdx == 0 || target[targetIdx-1] == '/' || target[targetIdx-1] == '_' || target[targetIdx-1] == '-' {
				score += 8
			}
			
			patternIdx++
		}
		targetIdx++
	}
	
	// Must match all pattern characters
	if patternIdx < len(pattern) {
		return 0, nil
	}
	
	// Bonus for shorter targets
	if len(target) > 0 {
		score += (100 - len(target)) / 10
	}
	
	// Bonus for filename matches vs path matches
	filename := filepath.Base(target)
	if strings.Contains(strings.ToLower(filename), pattern) {
		score += 20
	}
	
	return score, matches
}

// Search performs fuzzy search on indexed files
func (fm *FuzzyMatcher) Search(pattern string, maxResults int) []MatchResult {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	
	if pattern == "" {
		return nil
	}
	
	results := []MatchResult{}
	
	for _, file := range fm.files {
		// Try matching against filename first
		score, matches := calculateFuzzyScore(pattern, file.Name)
		if score == 0 {
			// Try matching against relative path
			score, matches = calculateFuzzyScore(pattern, file.RelPath)
		}
		
		if score > 0 {
			results = append(results, MatchResult{
				File:    file,
				Score:   score,
				Matches: matches,
			})
		}
	}
	
	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			// Secondary sort by path length (shorter first)
			return len(results[i].File.RelPath) < len(results[j].File.RelPath)
		}
		return results[i].Score > results[j].Score
	})
	
	// Limit results
	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}
	
	return results
}

// GetFileCount returns the number of indexed files
func (fm *FuzzyMatcher) GetFileCount() int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return len(fm.files)
}

// GetLastUpdate returns when the index was last updated
func (fm *FuzzyMatcher) GetLastUpdate() time.Time {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.lastUpdate
}