package adt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// --- Grep/Search Tools ---

// GrepMatch represents a single match in a grep search.
type GrepMatch struct {
	LineNumber    int      `json:"lineNumber"`
	MatchedLine   string   `json:"matchedLine"`
	ContextBefore []string `json:"contextBefore,omitempty"`
	ContextAfter  []string `json:"contextAfter,omitempty"`
	// Source is the include or function module the match is in, when the
	// object's code spans more than one source; LineNumber counts within it.
	Source string `json:"source,omitempty"`
}

// GrepObjectResult represents the result of grepping a single ABAP object.
type GrepObjectResult struct {
	Success    bool        `json:"success"`
	ObjectURL  string      `json:"objectUrl"`
	ObjectName string      `json:"objectName"`
	ObjectType string      `json:"objectType,omitempty"`
	Matches    []GrepMatch `json:"matches"`
	MatchCount int         `json:"matchCount"`
	Message    string      `json:"message,omitempty"`
	// Unsearched names the includes or function modules that could not be read.
	Unsearched []Unsearched `json:"unsearched,omitempty"`
}

// GrepPackageResult represents the result of grepping an ABAP package.
type GrepPackageResult struct {
	Success      bool               `json:"success"`
	PackageName  string             `json:"packageName"`
	Objects      []GrepObjectResult `json:"objects"`
	TotalMatches int                `json:"totalMatches"`
	Message      string             `json:"message,omitempty"`
	// Searched counts the objects read, by type: the coverage of the answer.
	Searched map[string]int `json:"searched,omitempty"`
	// Unsearched names the objects this sweep could not look at. Without it,
	// "no matches found in 50 objects" is said about a search that reached
	// thirty.
	Unsearched []Unsearched `json:"unsearched,omitempty"`
	// Partial is true when the answer does not cover everything asked for.
	Partial bool `json:"partial,omitempty"`
}

// GrepObject searches for a regex pattern in a single ABAP object's source code.
//
// Parameters:
//   - objectURL: ADT URL of the object (e.g., /sap/bc/adt/programs/programs/ZTEST)
//   - pattern: Regular expression pattern (Go regexp syntax)
//   - caseInsensitive: If true, perform case-insensitive matching
//   - contextLines: Number of lines to include before/after each match (default: 0)
//
// Returns matches with line numbers and optional context lines.
func (c *Client) GrepObject(ctx context.Context, objectURL, pattern string, caseInsensitive bool, contextLines int) (*GrepObjectResult, error) {
	result := &GrepObjectResult{
		ObjectURL: objectURL,
		Matches:   []GrepMatch{},
	}

	// Extract object name from URL
	parts := strings.Split(objectURL, "/")
	if len(parts) > 0 {
		result.ObjectName = parts[len(parts)-1]
	}

	// Compile regex pattern
	regexPattern := pattern
	if caseInsensitive {
		regexPattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		result.Message = fmt.Sprintf("Invalid regex pattern: %v", err)
		return result, nil
	}

	// Get source code
	sourceURLs, sources, missed, err := c.grepSources(ctx, objectURL)
	if err == nil && len(sources) == 0 && len(missed) > 0 {
		err = errors.New(missed[0].Reason)
	}
	if err != nil {
		result.Message = fmt.Sprintf("Failed to read source: %v", err)
		return result, nil
	}
	result.Unsearched = missed

	for s, source := range sources {
		// ADT sends CRLF; a CR left on each line keeps $ from ever matching.
		lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")

		// Search for matches
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			match := GrepMatch{
				LineNumber:  i + 1, // 1-based line numbers
				MatchedLine: line,
			}
			if len(sources) > 1 {
				match.Source = sourceURLs[s]
			}

			// Add context lines before
			if contextLines > 0 {
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				match.ContextBefore = lines[start:i]
			}

			// Add context lines after
			if contextLines > 0 {
				end := i + contextLines + 1
				if end > len(lines) {
					end = len(lines)
				}
				match.ContextAfter = lines[i+1 : end]
			}

			result.Matches = append(result.Matches, match)
		}
	}

	result.MatchCount = len(result.Matches)
	result.Success = true

	if result.MatchCount == 0 {
		result.Message = "No matches found"
	} else {
		result.Message = fmt.Sprintf("Found %d match(es) in %s", result.MatchCount, result.ObjectName)
	}
	if len(sources) > 1 {
		result.Message += fmt.Sprintf(" (%d sources read)", len(sources))
	}
	if note := UnsearchedNote(missed, len(sources)+len(missed), "source"); note != "" {
		result.Message += "\n" + note
	}

	return result, nil
}

var functionGroupURL = regexp.MustCompile(`(?i)^/sap/bc/adt/functions/groups/([^/]+)$`)

// grepSources reads every source an object's code lives in. A function group's
// /source/main is only the SAPL frame of INCLUDE lines and a class's is only
// the global class, so both are expanded into their includes (and function
// modules). An explicit .../source/main or class include URL is read as given.
func (c *Client) grepSources(ctx context.Context, objectURL string) (urls, bodies []string, missed []Unsearched, err error) {
	if m := functionGroupURL.FindStringSubmatch(objectURL); m != nil {
		name, _ := url.PathUnescape(m[1])
		return c.functionGroupSources(ctx, name)
	}
	isClass := strings.Contains(objectURL, "/oo/classes/")
	urls = []string{objectURL}
	if !strings.HasSuffix(objectURL, "/source/main") && !(isClass && strings.Contains(objectURL, "/includes/")) {
		urls[0] += "/source/main"
		if isClass {
			for _, inc := range []string{"definitions", "implementations", "macros", "testclasses"} {
				urls = append(urls, objectURL+"/includes/"+inc)
			}
		}
	}

	var read []string
	for i, u := range urls {
		resp, err := c.transport.Request(ctx, u, &RequestOptions{Method: "GET", Accept: "text/plain"})
		var apiErr *APIError
		switch {
		case i == 0 && err != nil:
			return nil, nil, nil, err
		case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound:
			// The class has no such include.
		case err != nil:
			missed = append(missed, Unsearched{Object: u, Reason: err.Error()})
		default:
			read = append(read, u)
			bodies = append(bodies, string(resp.Body))
		}
	}
	return read, bodies, missed, nil
}

// GrepObjectsResult represents the result of grepping multiple ABAP objects.
type GrepObjectsResult struct {
	Success      bool               `json:"success"`
	Objects      []GrepObjectResult `json:"objects"`
	TotalMatches int                `json:"totalMatches"`
	Message      string             `json:"message,omitempty"`
	// Unsearched names what this sweep could not look at. Without it, a
	// message counting every input describes a search that did not reach them.
	Unsearched []Unsearched `json:"unsearched,omitempty"`
	// Partial is true when the answer does not cover everything asked for.
	Partial bool `json:"partial,omitempty"`
}

// GrepObjects searches for a regex pattern in multiple ABAP objects' source code.
// This is a unified tool that handles both single and multiple object searches.
//
// Parameters:
//   - objectURLs: Array of ADT URLs (e.g., ["/sap/bc/adt/programs/programs/ZTEST"])
//   - pattern: Regular expression pattern (Go regexp syntax)
//   - caseInsensitive: If true, perform case-insensitive matching
//   - contextLines: Number of lines to include before/after each match (default: 0)
//
// Returns aggregated matches across all objects with per-object breakdown.
func (c *Client) GrepObjects(ctx context.Context, objectURLs []string, pattern string, caseInsensitive bool, contextLines int) (*GrepObjectsResult, error) {
	result := &GrepObjectsResult{
		Objects: []GrepObjectResult{},
	}

	if len(objectURLs) == 0 {
		result.Message = "No object URLs provided"
		return result, nil
	}

	// Search each object
	searched := 0
	for _, objectURL := range objectURLs {
		objResult, err := c.GrepObject(ctx, objectURL, pattern, caseInsensitive, contextLines)
		if err == nil && !objResult.Success {
			// GrepObject reports a failed read in its result, not as an error.
			err = errors.New(objResult.Message)
		}
		if err != nil {
			// Carrying on is right — one unreadable object should not lose the
			// other forty-nine — but it has to be recorded, or the count below
			// describes a search that did not happen.
			result.Unsearched = append(result.Unsearched, Unsearched{Object: objectURL, Reason: err.Error()})
			continue
		}
		searched++
		result.Unsearched = append(result.Unsearched, objResult.Unsearched...)

		// Only include objects with matches
		if objResult.MatchCount > 0 {
			result.Objects = append(result.Objects, *objResult)
			result.TotalMatches += objResult.MatchCount
		}
	}

	result.Success = searched > 0
	result.Partial = len(result.Unsearched) > 0
	if result.TotalMatches == 0 {
		result.Message = fmt.Sprintf("No matches found in %d object(s)", searched)
	} else {
		result.Message = fmt.Sprintf("Found %d match(es) across %d object(s)", result.TotalMatches, len(result.Objects))
	}
	if note := UnsearchedNote(result.Unsearched, searched+len(result.Unsearched), "object"); note != "" {
		result.Message += "\n" + note
	}

	return result, nil
}

// GrepPackage searches for a regex pattern across all objects in an ABAP package.
//
// Parameters:
//   - packageName: Name of the package (e.g., $TMP, ZPACKAGE)
//   - pattern: Regular expression pattern (Go regexp syntax)
//   - caseInsensitive: If true, perform case-insensitive matching
//   - objectTypes: Filter by object types (e.g., ["CLAS/OC", "PROG/P"]). Empty = search all.
//   - maxResults: Maximum number of matching objects to return (0 = unlimited)
//
// Returns matches grouped by object with match counts.
func (c *Client) GrepPackage(ctx context.Context, packageName, pattern string, caseInsensitive bool, objectTypes []string, maxResults int) (*GrepPackageResult, error) {
	result := &GrepPackageResult{
		PackageName: packageName,
		Objects:     []GrepObjectResult{},
		Searched:    map[string]int{},
	}

	fail := func(reason string) (*GrepPackageResult, error) {
		result.Message = reason
		result.Unsearched = []Unsearched{{Object: packageName, Reason: reason}}
		result.Partial = true
		return result, nil
	}

	// Get package contents
	packageContent, err := c.GetPackage(ctx, packageName)
	if err != nil {
		return fail(fmt.Sprintf("Failed to read package: %v", err))
	}
	// SAP answers a package that does not exist with the same empty tree as
	// an empty one; only a direct probe tells them apart.
	if len(packageContent.Objects) == 0 && len(packageContent.SubPackages) == 0 {
		if exists, err := c.PackageExists(ctx, packageName); err != nil {
			return fail(fmt.Sprintf("Package %s returned no objects, and whether it exists could not be checked: %v", packageName, err))
		} else if !exists {
			return fail(fmt.Sprintf("Package %s does not exist", packageName))
		}
	}

	// Build object type filter map
	typeFilter := make(map[string]bool)
	if len(objectTypes) > 0 {
		for _, t := range objectTypes {
			typeFilter[t] = true
		}
	}

	// Search each object in package
	var objs []PackageObject
	for _, obj := range packageContent.Objects {
		// Apply object type filter
		if len(typeFilter) > 0 && !typeFilter[obj.Type] {
			continue
		}

		// Skip non-source objects (tables, structures, etc.)
		if !isSourceObject(obj.Type) {
			continue
		}
		objs = append(objs, obj)
	}

	// Read the objects in a bounded pool until the deadline, then collect in
	// package order so the answer does not depend on timing.
	// ponytail: a function group fans out 6 more reads of its own, so the
	// worst case is 8x6 requests at once; share one limiter if SAP feels it.
	ctx, cancel := context.WithTimeout(ctx, grepPackageDeadline)
	defer cancel()
	objResults := make([]*GrepObjectResult, len(objs))
	errs := make([]error, len(objs))
	var found atomic.Int32
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, obj := range objs {
		sem <- struct{}{}
		if ctx.Err() != nil || (maxResults > 0 && int(found.Load()) >= maxResults) {
			break
		}
		wg.Add(1)
		go func(i int, uri string) {
			defer func() { <-sem; wg.Done() }()
			objResults[i], errs[i] = c.GrepObject(ctx, uri, pattern, caseInsensitive, 0)
			if errs[i] == nil && objResults[i].MatchCount > 0 {
				found.Add(1)
			}
		}(i, obj.URI)
	}
	wg.Wait()

	objectsSearched, searched := 0, 0
	capped := false
	for i, obj := range objs {
		objResult, err := objResults[i], errs[i]
		if objResult == nil && err == nil {
			// Never started: the deadline (or the caller) stopped the walk.
			err = fmt.Errorf("not searched: %w", ctx.Err())
		}
		if err == nil && !objResult.Success {
			err = errors.New(objResult.Message)
		}
		if err != nil {
			result.Unsearched = append(result.Unsearched, Unsearched{Object: obj.URI, Reason: err.Error()})
			continue
		}
		searched++
		result.Searched[obj.Type]++
		result.Unsearched = append(result.Unsearched, objResult.Unsearched...)

		// Only include objects with matches
		if objResult.MatchCount > 0 {
			objResult.ObjectType = obj.Type
			result.Objects = append(result.Objects, *objResult)
			result.TotalMatches += objResult.MatchCount

			// Check max results limit
			objectsSearched++
			if maxResults > 0 && objectsSearched >= maxResults {
				capped = true
				break
			}
		}
	}

	result.Success = searched > 0 || len(objs) == 0
	result.Partial = len(result.Unsearched) > 0 || capped
	if result.TotalMatches == 0 {
		result.Message = "No matches found in package"
	} else {
		result.Message = fmt.Sprintf("Found %d match(es) across %d object(s) in package %s",
			result.TotalMatches, len(result.Objects), packageName)
	}
	if capped {
		result.Message += maxResultsNote(maxResults)
	}
	if note := UnsearchedNote(result.Unsearched, searched+len(result.Unsearched), "object"); note != "" {
		result.Message += "\n" + note
	}

	return result, nil
}

// GrepPackagesResult represents the result of grepping multiple ABAP packages.
type GrepPackagesResult struct {
	Success      bool               `json:"success"`
	Packages     []string           `json:"packages"` // List of searched packages
	Objects      []GrepObjectResult `json:"objects"`
	TotalMatches int                `json:"totalMatches"`
	Message      string             `json:"message,omitempty"`
	// Searched counts the objects read, by type: the coverage of the answer.
	Searched map[string]int `json:"searched,omitempty"`
	// Unsearched names what this sweep could not look at. Without it, a
	// message counting every input describes a search that did not reach them.
	Unsearched []Unsearched `json:"unsearched,omitempty"`
	// Partial is true when the answer does not cover everything asked for.
	Partial bool `json:"partial,omitempty"`
}

// GrepPackages searches for a regex pattern across multiple ABAP packages.
// This is a unified tool that handles single, multiple, and recursive package searches.
//
// Parameters:
//   - packages: Array of package names (e.g., ["$TMP"], ["$TMP", "ZLOCAL"])
//   - includeSubpackages: If true, recursively search all subpackages
//   - pattern: Regular expression pattern (Go regexp syntax)
//   - caseInsensitive: If true, perform case-insensitive matching
//   - objectTypes: Filter by object types (e.g., ["CLAS/OC", "PROG/P"]). Empty = search all.
//   - maxResults: Maximum number of matching objects to return (0 = unlimited)
//
// Returns aggregated matches across all packages with per-object breakdown.
func (c *Client) GrepPackages(ctx context.Context, packages []string, includeSubpackages bool, pattern string, caseInsensitive bool, objectTypes []string, maxResults int) (*GrepPackagesResult, error) {
	result := &GrepPackagesResult{
		Packages: []string{},
		Objects:  []GrepObjectResult{},
		Searched: map[string]int{},
	}
	ctx, cancel := context.WithTimeout(ctx, grepPackageDeadline)
	defer cancel()

	if len(packages) == 0 {
		result.Message = "No packages provided"
		return result, nil
	}

	// Collect all packages to search (including subpackages if requested)
	packagesToSearch := []string{}
	for _, pkg := range packages {
		if includeSubpackages {
			// Get package tree (including subpackages)
			subPackages, err := c.collectSubpackages(ctx, pkg)
			if err != nil {
				// If error getting subpackages, just search the main package
				packagesToSearch = append(packagesToSearch, pkg)
			} else {
				packagesToSearch = append(packagesToSearch, subPackages...)
			}
		} else {
			packagesToSearch = append(packagesToSearch, pkg)
		}
	}

	result.Packages = packagesToSearch

	// Search each package
	totalObjectsSearched, searchedPackages := 0, 0
	capped := false
	for _, packageName := range packagesToSearch {
		pkgResult, err := c.GrepPackage(ctx, packageName, pattern, caseInsensitive, objectTypes, maxResults-totalObjectsSearched)
		if err == nil && !pkgResult.Success {
			err = errors.New(pkgResult.Message)
		}
		if err != nil {
			result.Unsearched = append(result.Unsearched, Unsearched{Object: packageName, Reason: err.Error()})
			continue
		}

		searchedPackages++
		// A package that was searched only partly passes its own gaps up:
		// they are the caller's gaps too.
		result.Unsearched = append(result.Unsearched, pkgResult.Unsearched...)
		for t, n := range pkgResult.Searched {
			result.Searched[t] += n
		}
		result.Objects = append(result.Objects, pkgResult.Objects...)
		result.TotalMatches += pkgResult.TotalMatches
		totalObjectsSearched += len(pkgResult.Objects)

		// Check if we've reached max results
		if maxResults > 0 && totalObjectsSearched >= maxResults {
			capped = true
			break
		}
	}

	result.Success = searchedPackages > 0
	result.Partial = len(result.Unsearched) > 0 || capped
	if result.TotalMatches == 0 {
		result.Message = fmt.Sprintf("No matches found in %d package(s)", searchedPackages)
	} else {
		result.Message = fmt.Sprintf("Found %d match(es) across %d object(s) in %d package(s)",
			result.TotalMatches, len(result.Objects), searchedPackages)
	}
	if capped {
		result.Message += maxResultsNote(maxResults)
	}
	if note := UnsearchedNote(result.Unsearched, searchedPackages+len(result.Unsearched), "item"); note != "" {
		result.Message += "\n" + note
	}

	return result, nil
}

// grepPackageDeadline bounds a package grep: past it, the objects not yet read
// are reported as unsearched instead of the call running on for minutes.
var grepPackageDeadline = 90 * time.Second

// maxResultsNote says that max_results ended the walk, so the answer is not
// known to be complete.
func maxResultsNote(maxResults int) string {
	return fmt.Sprintf("\nStopped at max_results=%d matching objects; the rest was not searched, so more may match (raise max_results or narrow the pattern).", maxResults)
}

// collectSubpackages recursively collects a package and all its subpackages.
func (c *Client) collectSubpackages(ctx context.Context, packageName string) ([]string, error) {
	packages := []string{packageName}

	// Get package contents
	content, err := c.GetPackage(ctx, packageName)
	if err != nil {
		return packages, err
	}

	// Check if package content has subpackages
	// PackageContent has a SubPackages field ([]string) if it exists
	if content.SubPackages != nil && len(content.SubPackages) > 0 {
		for _, subpkgName := range content.SubPackages {
			// Recursively collect subpackages
			subPackages, err := c.collectSubpackages(ctx, subpkgName)
			if err == nil {
				packages = append(packages, subPackages...)
			}
		}
	}

	return packages, nil
}

// isSourceObject returns true if the object type contains source code that can be searched.
func isSourceObject(objectType string) bool {
	sourceTypes := map[string]bool{
		"PROG/P":  true, // Programs
		"CLAS/OC": true, // Classes
		"INTF/OI": true, // Interfaces
		"FUGR/F":  true, // Function groups
		"FUGR/FF": true, // Function modules
		"PROG/I":  true, // Includes
	}
	return sourceTypes[objectType]
}
