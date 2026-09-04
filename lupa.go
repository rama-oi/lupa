package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type Config struct {
	Depth       int
	Exclude     []string
	Include     []string
	NoColor     bool
	Summary     bool
}

type File struct {
	ID      int
	Path    string
	Name    string
	Size    int64
	Lines   int
	Binary  bool
	Content string
}

func main() {
	var excludes stringList
	var includes stringList

	depth := flag.Int(
		"depth",
		-1,
		"maximum directory depth",
	)

	flag.Var(
		&excludes,
		"exclude",
		"exclude file or directory (repeatable)",
	)

	flag.Var(
		&includes,
		"include",
		"include matching files (repeatable)",
	)

	noColor := flag.Bool(
		"no-color",
		false,
		"disable colors",
	)

	summary := flag.Bool(
		"summary",
		false,
		"show summaries without file contents",
	)

	flag.Usage = usage
	flag.Parse()

	root := "."

	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	config := Config{
		Depth:       *depth,
		Exclude:     excludes,
		Include:     includes,
		NoColor:     *noColor,
		Summary:     *summary,
	}

	if err := Run(root, config); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage:
  ctx [path] [options]

Options:
  --depth <n>             Maximum directory depth
  --exclude <pattern>     Exclude file/directory (repeatable)
  --include <pattern>     Include matching files (repeatable)
  --no-color              Disable terminal colors
  --summary               Show summaries without file contents

Examples:
  ctx .
  ctx ./src --depth 4
  ctx . --exclude node_modules --exclude .git
  ctx . --include "*.py"
  ctx . --summary
`)
}

// ------------------------------------------------------------
// Run
// ------------------------------------------------------------

func Run(root string, config Config) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	info, err := os.Stat(root)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}

	files, err := collect(root, config)
	if err != nil {
		return err
	}

	for _, file := range files {
		printFile(file, config)
	}

	return nil
}

// ------------------------------------------------------------
// File collection
// ------------------------------------------------------------

func collect(root string, config Config) ([]File, error) {
	var files []File

	err := filepath.WalkDir(root, func(
		fullPath string,
		entry fs.DirEntry,
		err error,
	) error {

		if err != nil {
			return err
		}

		if fullPath == root {
			return nil
		}

		relative, err := filepath.Rel(root, fullPath)
		if err != nil {
			return err
		}

		relative = filepath.ToSlash(relative)

		depth := pathDepth(relative)

		// Depth limit.
		if config.Depth >= 0 && depth > config.Depth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Excluded paths.
		if matchesExclude(relative, entry.Name(), config.Exclude) {
			if entry.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		// Directories are added to the tree.
		if entry.IsDir() {
			return nil
		}

		// Include filtering only applies to files.
		if len(config.Include) > 0 &&
			!matchesInclude(relative, entry.Name(), config.Include) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		file := File{
			ID:   len(files) + 1,
			Path: "./" + relative,
			Name: entry.Name(),
			Size: info.Size(),
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			fmt.Fprintf(
				os.Stderr,
				"warning: cannot read %s: %v\n",
				relative,
				err,
			)

			return nil
		}

		file.Binary = isBinary(data)

		if file.Binary {
			file.Content = "BINARY FILE"
		} else {
			file.Content = string(data)
			file.Lines = countLines(file.Content)
		}

		files = append(files, file)

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// ------------------------------------------------------------
// File output
// ------------------------------------------------------------

func printFile(file File, config Config) {
	summary := fmt.Sprintf(
		"------------------ %d::[%s]::SUMMARY",
		file.ID,
		file.Name,
	)

	begin := fmt.Sprintf(
		"------------------ %d::[%s]::BEGIN",
		file.ID,
		file.Name,
	)

	end := fmt.Sprintf(
		"------------------ %d::[%s]::END",
		file.ID,
		file.Name,
	)

	fmt.Println(colorize(summary, Cyan, config.NoColor))

	fmt.Printf("PATH: %s\n", file.Path)

	if !file.Binary {
		fmt.Printf("LINES: %d\n", file.Lines)
	}

	fmt.Printf("SIZE: %s\n", formatSize(file.Size))

	if config.Summary {
		fmt.Println()
		return
	}

	fmt.Println(
		colorize(begin, Dim, config.NoColor),
	)

	if file.Binary {
		fmt.Println(
			colorize("BINARY FILE", Red, config.NoColor),
		)
	} else {
		fmt.Print(file.Content)

		if !strings.HasSuffix(file.Content, "\n") {
			fmt.Println()
		}
	}

	fmt.Println(
		colorize(end, Dim, config.NoColor),
	)

	fmt.Println()
}

// ------------------------------------------------------------
// Matching
// ------------------------------------------------------------

func matchesExclude(
	relative string,
	name string,
	patterns []string,
) bool {

	for _, pattern := range patterns {
		pattern = filepath.ToSlash(pattern)
		pattern = strings.TrimPrefix(pattern, "/")

		// Filename match.
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}

		// Complete relative path.
		if ok, _ := path.Match(pattern, relative); ok {
			return true
		}

		// Directory component.
		parts := strings.Split(relative, "/")

		for _, part := range parts {
			if ok, _ := path.Match(pattern, part); ok {
				return true
			}
		}
	}

	return false
}

func matchesInclude(
	relative string,
	name string,
	patterns []string,
) bool {

	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}

		if ok, _ := path.Match(pattern, relative); ok {
			return true
		}
	}

	return false
}

// ------------------------------------------------------------
// Binary detection
// ------------------------------------------------------------

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}

	return false
}

// ------------------------------------------------------------
// Line counting
// ------------------------------------------------------------

func countLines(content string) int {
	if len(content) == 0 {
		return 0
	}

	return 1 + strings.Count(content, "\n")
}

// ------------------------------------------------------------
// Size formatting
// ------------------------------------------------------------

func formatSize(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)

	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)

	case size < 1024*1024*1024:
		return fmt.Sprintf(
			"%.1f MB",
			float64(size)/(1024*1024),
		)

	default:
		return fmt.Sprintf(
			"%.1f GB",
			float64(size)/(1024*1024*1024),
		)
	}
}

// ------------------------------------------------------------
// Path depth
// ------------------------------------------------------------

func pathDepth(p string) int {
	p = strings.Trim(p, "/")

	if p == "" {
		return 0
	}

	return strings.Count(p, "/") + 1
}

// ------------------------------------------------------------
// Output sections
// ------------------------------------------------------------

func printSectionHeader(title string, config Config) {
	if !config.NoColor {
		fmt.Printf("\033[1;36m%s\033[0m\n\n", title)
	} else {
		fmt.Println(title)
		fmt.Println()
	}
}

// ------------------------------------------------------------
// Colors
// ------------------------------------------------------------

const (
	Reset = "\033[0m"
	Cyan  = "\033[36m"
	Dim   = "\033[2m"
	Red   = "\033[31m"
)

func colorize(
	text string,
	color string,
	disabled bool,
) string {
	if disabled {
		return text
	}

	return color + text + Reset
}
