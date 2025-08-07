package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCalculateFuzzyScore(t *testing.T) {
	tests := []struct {
		name           string
		pattern        string
		target         string
		expectMatch    bool
		expectMinScore int
		expectExact    bool
	}{
		{
			name:        "exact match",
			pattern:     "test",
			target:      "test",
			expectMatch: true,
			expectExact: true,
		},
		{
			name:           "case insensitive exact match",
			pattern:        "TEST",
			target:         "test",
			expectMatch:    true,
			expectExact:    true,
			expectMinScore: 1000,
		},
		{
			name:           "consecutive characters",
			pattern:        "main",
			target:         "main.go",
			expectMatch:    true,
			expectMinScore: 40,
		},
		{
			name:           "scattered characters",
			pattern:        "mgo",
			target:         "main.go",
			expectMatch:    true,
			expectMinScore: 30,
		},
		{
			name:           "start of word bonus",
			pattern:        "fm",
			target:         "fuzzy_matcher.go",
			expectMatch:    true,
			expectMinScore: 20,
		},
		{
			name:           "path separator bonus",
			pattern:        "cmd",
			target:         "src/cmd/main.go",
			expectMatch:    true,
			expectMinScore: 30,
		},
		{
			name:        "no match",
			pattern:     "xyz",
			target:      "main.go",
			expectMatch: false,
		},
		{
			name:        "partial match failure",
			pattern:     "mainx",
			target:      "main.go",
			expectMatch: false,
		},
		{
			name:           "simple pattern match",
			pattern:        "stylcss",
			target:         "stylesheets/application.css",
			expectMatch:    true,
			expectMinScore: 30,
		},
		{
			name:        "empty pattern",
			pattern:     "",
			target:      "test.go",
			expectMatch: false,
		},
		{
			name:           "filename bonus",
			pattern:        "test",
			target:         "path/to/test.go",
			expectMatch:    true,
			expectMinScore: 60,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, matches := calculateFuzzyScore(tt.pattern, tt.target)
			
			if tt.expectMatch {
				if score == 0 {
					t.Errorf("expected match for pattern '%s' in target '%s', but got score 0", tt.pattern, tt.target)
				}
				if len(matches) != len(tt.pattern) {
					t.Errorf("expected %d matches for pattern '%s', but got %d", len(tt.pattern), tt.pattern, len(matches))
				}
				if tt.expectExact && score != 1000 {
					t.Errorf("expected exact match score 1000, but got %d", score)
				}
				if tt.expectMinScore > 0 && score < tt.expectMinScore {
					t.Errorf("expected minimum score %d, but got %d", tt.expectMinScore, score)
				}
			} else {
				if score != 0 {
					t.Errorf("expected no match for pattern '%s' in target '%s', but got score %d", tt.pattern, tt.target, score)
				}
				if len(matches) != 0 {
					t.Errorf("expected no matches, but got %d", len(matches))
				}
			}
		})
	}
}

func TestFuzzyMatcherExclusions(t *testing.T) {
	fm := NewFuzzyMatcher("/test")
	
	tests := []struct {
		name     string
		path     string
		filename string
		isDir    bool
		exclude  bool
	}{
		{"node_modules directory", "project/node_modules", "node_modules", true, true},
		{"directory in node_modules", "project/node_modules/pkg", "pkg", true, true},
		{"file not excluded by name", "project/node_modules/index.js", "index.js", false, false},
		{".git directory", ".git", ".git", true, true},
		{"hidden file", ".hidden", ".hidden", false, true},
		{".gitignore allowed", ".gitignore", ".gitignore", false, false},
		{".env file allowed", ".env", ".env", false, false},
		{".env.local allowed", ".env.local", ".env.local", false, false},
		{"log file", "app.log", "app.log", false, true},
		{"temp file", "temp.tmp", "temp.tmp", false, true},
		{"swap file", "file.swp", "file.swp", false, true},
		{"DS_Store", ".DS_Store", ".DS_Store", false, true},
		{"normal file", "main.go", "main.go", false, false},
		{"elm-stuff directory", "elm-stuff", "elm-stuff", true, true},
		{"build directory", "build", "build", true, true},
		{"dist directory", "dist", "dist", true, true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fm.shouldExclude(tt.path, tt.filename, tt.isDir)
			if result != tt.exclude {
				t.Errorf("shouldExclude(%s, %s, %v) = %v, want %v", 
					tt.path, tt.filename, tt.isDir, result, tt.exclude)
			}
		})
	}
}

func TestFuzzyMatcherIndexing(t *testing.T) {
	tmpDir := t.TempDir()
	
	testFiles := []struct {
		path  string
		isDir bool
	}{
		{"main.go", false},
		{"README.md", false},
		{"src", true},
		{"src/app.go", false},
		{"src/utils.go", false},
		{".gitignore", false},
		{".git", true},
		{".git/config", false},
		{"node_modules", true},
		{"node_modules/pkg/index.js", false},
		{".hidden.txt", false},
		{"test.log", false},
		{"build", true},
		{"build/output.js", false},
	}
	
	for _, tf := range testFiles {
		fullPath := filepath.Join(tmpDir, tf.path)
		if tf.isDir {
			os.MkdirAll(fullPath, 0755)
		} else {
			dir := filepath.Dir(fullPath)
			os.MkdirAll(dir, 0755)
			os.WriteFile(fullPath, []byte("test content"), 0644)
		}
	}
	
	fm := NewFuzzyMatcher(tmpDir)
	err := fm.IndexFiles()
	if err != nil {
		t.Fatalf("IndexFiles failed: %v", err)
	}
	
	excludedPaths := []string{
		".git/config",
		"node_modules/pkg/index.js",
		".hidden.txt",
		"test.log",
		"build/output.js",
	}
	
	includedPaths := []string{
		"main.go",
		"README.md",
		"src/app.go",
		"src/utils.go",
		".gitignore",
	}
	
	fileCount := fm.GetFileCount()
	expectedCount := len(includedPaths) + 1
	if fileCount != expectedCount {
		t.Errorf("expected %d files indexed, got %d", expectedCount, fileCount)
	}
	
	for _, path := range excludedPaths {
		results := fm.Search(filepath.Base(path), 10)
		for _, result := range results {
			if strings.Contains(result.File.RelPath, path) {
				t.Errorf("excluded file %s should not be indexed, but found in results", path)
			}
		}
	}
	
	for _, path := range includedPaths {
		results := fm.Search(filepath.Base(path), 10)
		found := false
		for _, result := range results {
			if result.File.RelPath == path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("included file %s should be indexed, but not found in results", path)
		}
	}
}

func TestFuzzyMatcherSearch(t *testing.T) {
	fm := &FuzzyMatcher{
		rootDir: "/test",
		files: []FileInfo{
			{Path: "/test/main.go", Name: "main.go", RelPath: "main.go", IsDir: false},
			{Path: "/test/cmd/main.go", Name: "main.go", RelPath: "cmd/main.go", IsDir: false},
			{Path: "/test/internal/app/main_test.go", Name: "main_test.go", RelPath: "internal/app/main_test.go", IsDir: false},
			{Path: "/test/fuzzy_matcher.go", Name: "fuzzy_matcher.go", RelPath: "fuzzy_matcher.go", IsDir: false},
			{Path: "/test/README.md", Name: "README.md", RelPath: "README.md", IsDir: false},
			{Path: "/test/app/assets/stylesheets/application.css", Name: "application.css", RelPath: "app/assets/stylesheets/application.css", IsDir: false},
		},
	}
	
	tests := []struct {
		name          string
		pattern       string
		maxResults    int
		expectCount   int
		expectFirst   string
		checkOrdering bool
	}{
		{
			name:        "search for main",
			pattern:     "main",
			maxResults:  5,
			expectCount: 3,
			expectFirst: "main.go",
			checkOrdering: true,
		},
		{
			name:        "search for fuzzy",
			pattern:     "fuzzy",
			maxResults:  5,
			expectCount: 1,
			expectFirst: "fuzzy_matcher.go",
		},
		{
			name:        "no complex pattern matches",
			pattern:     "statstylcss",
			maxResults:  5,
			expectCount: 0,
		},
		{
			name:        "no matches",
			pattern:     "xyz",
			maxResults:  5,
			expectCount: 0,
		},
		{
			name:        "empty pattern",
			pattern:     "",
			maxResults:  5,
			expectCount: 0,
		},
		{
			name:        "limit results",
			pattern:     "go",
			maxResults:  2,
			expectCount: 2,
		},
		{
			name:        "case insensitive",
			pattern:     "README",
			maxResults:  5,
			expectCount: 1,
			expectFirst: "README.md",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := fm.Search(tt.pattern, tt.maxResults)
			
			if len(results) != tt.expectCount {
				t.Errorf("expected %d results, got %d", tt.expectCount, len(results))
			}
			
			if tt.expectFirst != "" && len(results) > 0 {
				if results[0].File.RelPath != tt.expectFirst {
					t.Errorf("expected first result to be %s, got %s", tt.expectFirst, results[0].File.RelPath)
				}
			}
			
			if tt.checkOrdering && len(results) > 1 {
				for i := 1; i < len(results); i++ {
					if results[i].Score > results[i-1].Score {
						t.Errorf("results not properly ordered by score: %d > %d at index %d", 
							results[i].Score, results[i-1].Score, i)
					}
				}
			}
		})
	}
}

func TestFuzzyMatcherConcurrency(t *testing.T) {
	fm := NewFuzzyMatcher("/test")
	fm.files = []FileInfo{
		{Path: "/test/main.go", Name: "main.go", RelPath: "main.go"},
		{Path: "/test/test.go", Name: "test.go", RelPath: "test.go"},
	}
	
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			results := fm.Search("main", 5)
			if len(results) == 0 {
				t.Error("concurrent search failed")
			}
			done <- true
		}()
	}
	
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestFuzzyMatcherGetters(t *testing.T) {
	fm := NewFuzzyMatcher("/test")
	
	if fm.GetFileCount() != 0 {
		t.Errorf("expected initial file count 0, got %d", fm.GetFileCount())
	}
	
	if !fm.GetLastUpdate().IsZero() {
		t.Error("expected initial last update to be zero time")
	}
	
	fm.files = []FileInfo{
		{Path: "/test/a.go", Name: "a.go", RelPath: "a.go"},
		{Path: "/test/b.go", Name: "b.go", RelPath: "b.go"},
	}
	fm.lastUpdate = time.Now()
	
	if fm.GetFileCount() != 2 {
		t.Errorf("expected file count 2, got %d", fm.GetFileCount())
	}
	
	if fm.GetLastUpdate().IsZero() {
		t.Error("expected last update to be non-zero after update")
	}
}

func TestGitignoreLoading(t *testing.T) {
	tmpDir := t.TempDir()
	
	gitignoreContent := `# Comments should be ignored
*.log
temp/
!important.log
`
	os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0644)
	
	fm := NewFuzzyMatcher(tmpDir)
	err := fm.loadGitignore()
	if err != nil {
		t.Fatalf("loadGitignore failed: %v", err)
	}
	
	expectedPatterns := []string{"*.log", "temp/", "!important.log"}
	loadedCount := 0
	for pattern := range fm.gitignore {
		found := false
		for _, expected := range expectedPatterns {
			if pattern == expected {
				found = true
				break
			}
		}
		if found {
			loadedCount++
		}
	}
	
	if loadedCount != len(expectedPatterns) {
		t.Errorf("expected %d gitignore patterns, got %d", len(expectedPatterns), loadedCount)
	}
}