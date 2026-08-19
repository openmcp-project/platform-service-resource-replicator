// embed-docs injects YAML file contents into fenced code blocks in markdown files.
// Markers in the markdown file indicate which file to inject:
//
//	<!-- inject: path/to/file.yaml -->
//	```yaml
//	... existing content (will be replaced) ...
//	```
//	<!-- end inject -->
//
// The path in the inject marker is relative to the markdown file's directory.
// Run with: go run ./hack/embed-docs [flags] <file.md|dir> [file.md|dir ...]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
)

var (
	reInjectStart = regexp.MustCompile(`^<!--\s*inject:\s*(.+?)\s*-->$`)
	reInjectEnd   = regexp.MustCompile(`^<!--\s*end inject\s*-->$`)
	reFenceOpen   = regexp.MustCompile("^```")
	reFenceClose  = regexp.MustCompile("^```$")
)

type status string

const (
	statusUnchanged status = "unchanged"
	statusModified  status = "modified"
	statusError     status = "error"
)

type fileResult struct {
	File    string `json:"file"`
	Status  status `json:"status"`
	Error   string `json:"error,omitempty"`
}

type output struct {
	Results []fileResult `json:"results"`
}

func main() {
	check := flag.Bool("check", false, "Check mode: exit with non-zero status if any file would be changed, but do not write changes.")
	jsonOut := flag.Bool("json", false, "Print output as JSON instead of YAML.")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go run ./hack/embed-docs [flags] <file.md|dir> [file.md|dir ...]\n\n")
		fmt.Fprintf(os.Stderr, "Injects YAML file contents into fenced code blocks in markdown files.\n")
		fmt.Fprintf(os.Stderr, "If a directory is given, all .md files in that directory are processed (non-recursively).\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	var results []fileResult
	anyDiff := false
	anyError := false

	var mdFiles []string
	for _, arg := range flag.Args() {
		info, err := os.Stat(arg)
		if err != nil {
			results = append(results, fileResult{File: arg, Status: statusError, Error: err.Error()})
			anyError = true
			continue
		}
		if info.IsDir() {
			entries, err := os.ReadDir(arg)
			if err != nil {
				results = append(results, fileResult{File: arg, Status: statusError, Error: err.Error()})
				anyError = true
				continue
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					mdFiles = append(mdFiles, filepath.Join(arg, e.Name()))
				}
			}
		} else {
			mdFiles = append(mdFiles, arg)
		}
	}

	for _, mdFile := range mdFiles {
		changed, err := processFile(mdFile, *check)
		switch {
		case err != nil:
			results = append(results, fileResult{File: mdFile, Status: statusError, Error: err.Error()})
			anyError = true
		case changed:
			results = append(results, fileResult{File: mdFile, Status: statusModified})
			anyDiff = true
		default:
			results = append(results, fileResult{File: mdFile, Status: statusUnchanged})
		}
	}

	if len(mdFiles) == 0 {
		fmt.Println("no markdown files found in the specified paths")
	} else {
		printOutput(output{Results: results}, *jsonOut)
	}

	if anyError || (*check && anyDiff) {
		os.Exit(1)
	}
}

func printOutput(out output, asJSON bool) {
	var (
		data []byte
		err  error
	)
	if asJSON {
		data, err = json.MarshalIndent(out, "", "  ")
		data = append(data, '\n')
	} else {
		data, err = yaml.Marshal(out)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshalling output: %v\n", err)
		return
	}
	fmt.Print(string(data))
}

func processFile(mdFile string, checkOnly bool) (changed bool, err error) {
	data, err := os.ReadFile(mdFile)
	if err != nil {
		return false, err
	}
	original := string(data)
	lines := strings.Split(original, "\n")
	result, err := inject(lines, filepath.Dir(mdFile))
	if err != nil {
		return false, err
	}
	updated := strings.Join(result, "\n")
	if updated == original {
		return false, nil
	}
	if checkOnly {
		return true, nil
	}
	return true, os.WriteFile(mdFile, []byte(updated), 0644)
}

func inject(lines []string, baseDir string) (result []string, err error) {
	i := 0
	for i < len(lines) {
		line := lines[i]

		m := reInjectStart.FindStringSubmatch(line)
		if m == nil {
			result = append(result, line)
			i++
			continue
		}

		// found inject marker
		injectFile := filepath.Join(baseDir, m[1])
		result = append(result, line) // keep the marker
		i++

		// expect opening fence
		if i >= len(lines) || !reFenceOpen.MatchString(lines[i]) {
			return nil, fmt.Errorf("expected opening fence after inject marker for %s, got: %q", injectFile, lines[i])
		}
		fenceLine := lines[i]
		result = append(result, fenceLine)
		i++

		// skip existing content until closing fence
		for i < len(lines) && !reFenceClose.MatchString(lines[i]) {
			i++
		}
		if i >= len(lines) {
			return nil, fmt.Errorf("missing closing fence for inject block referencing %s", injectFile)
		}

		// inject file contents
		fileData, err := os.ReadFile(injectFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read inject file %s: %w", injectFile, err)
		}
		injected := strings.TrimRight(string(fileData), "\n")
		result = append(result, injected)

		result = append(result, lines[i]) // closing fence
		i++

		// expect end inject marker
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			result = append(result, lines[i])
			i++
		}
		if i >= len(lines) || !reInjectEnd.MatchString(lines[i]) {
			return nil, fmt.Errorf("expected end inject marker after closing fence for %s", injectFile)
		}
		result = append(result, lines[i])
		i++
	}
	return result, nil
}
