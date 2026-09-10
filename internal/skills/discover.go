// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/googleapis/mcp-toolbox/internal/resources"
	"github.com/googleapis/mcp-toolbox/internal/util"
)

// skillFile is the file whose presence makes a directory a skill.
const skillFile = "SKILL.md"

// Discover builds one Entry per skill found in resourcesMap.
//
// A skill is any resource addressed at skill://<skill-path>/SKILL.md, however it
// was declared; every resource sharing that prefix is one of its supporting
// files. Recognising on SKILL.md rather than on a directory resource is what
// lets a skill be assembled from hand-written file and text resources, and it
// matches SEP-2640, which keys an entry on its SKILL.md URI and never on a root.
//
// Nested skills are returned as entries in their own right and remain listed in
// the enclosing skill's manifest, as the SEP requires.
func Discover(ctx context.Context, resourcesMap map[string]resources.Resource) ([]Entry, error) {
	roots := skillRoots(resourcesMap)
	if len(roots) == 0 {
		return nil, nil
	}

	entries := make([]Entry, 0, len(roots))
	for _, root := range roots {
		e, err := buildEntry(ctx, root, resourcesMap)
		if err != nil {
			return nil, err
		}
		if err := e.Validate(); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	if err := warnOnDuplicateNames(ctx, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// skillRoots returns the skill:// prefix of every SKILL.md in the map, sorted so
// that discovery does not inherit Go's randomised map iteration order.
func skillRoots(resourcesMap map[string]resources.Resource) []string {
	var roots []string
	for _, res := range resourcesMap {
		uri := res.GetURI()
		if !strings.HasPrefix(uri, resources.SkillScheme+"://") {
			continue
		}
		if root, ok := strings.CutSuffix(uri, "/"+skillFile); ok {
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots
}

// buildEntry hashes every file under root and assembles its entry.
func buildEntry(ctx context.Context, root string, resourcesMap map[string]resources.Resource) (Entry, error) {
	skillURI := root + "/" + skillFile

	members := make([]resources.Resource, 0, 8)
	for _, res := range resourcesMap {
		if strings.HasPrefix(res.GetURI(), root+"/") {
			members = append(members, res)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].GetURI() < members[j].GetURI() })

	refs := make([]ResourceRef, 0, len(members))
	var frontmatter map[string]any
	for _, res := range members {
		content, err := readString(ctx, res)
		if err != nil {
			return Entry{}, fmt.Errorf("skill %q: %w", skillURI, err)
		}
		sum := sha256.Sum256([]byte(content))
		refs = append(refs, ResourceRef{
			URI:    res.GetURI(),
			Digest: "sha256:" + hex.EncodeToString(sum[:]),
			Size:   int64(len(content)),
		})
		if res.GetURI() == skillURI {
			frontmatter, err = parseFrontmatter(content)
			if err != nil {
				return Entry{}, fmt.Errorf("skill %q: %w", skillURI, err)
			}
		}
	}

	return Entry{URI: skillURI, Frontmatter: frontmatter, Resources: Manifest{Refs: refs}}, nil
}

// readString reads a resource's content. Read returns any because a future
// resource type may not be textual; a skill's files must be.
func readString(ctx context.Context, res resources.Resource) (string, error) {
	got, err := res.Read(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("unable to read %q: %w", res.GetURI(), err)
	}
	content, ok := got.(string)
	if !ok {
		return "", fmt.Errorf("%q returned %T, want text content", res.GetURI(), got)
	}
	return content, nil
}

// parseFrontmatter extracts the leading YAML frontmatter of a SKILL.md. The
// Agent Skills specification requires it, so its absence is an error rather
// than an empty map.
func parseFrontmatter(content string) (map[string]any, error) {
	// Only the delimiters are normalised; the digest is taken over raw bytes.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return nil, fmt.Errorf("%s must open with YAML frontmatter delimited by ---", skillFile)
	}
	body, ok := cutAtDelimiter(rest)
	if !ok {
		return nil, fmt.Errorf("%s frontmatter is not closed by --- on a line of its own", skillFile)
	}

	fm := map[string]any{}
	if err := yaml.Unmarshal([]byte(body), &fm); err != nil {
		return nil, fmt.Errorf("unable to parse %s frontmatter: %w", skillFile, err)
	}
	return fm, nil
}

// cutAtDelimiter returns everything before the first line consisting only of
// ---, reporting whether such a line exists. Trailing whitespace is tolerated
// because an editor leaves it behind where the author cannot see it.
func cutAtDelimiter(rest string) (string, bool) {
	for offset := 0; ; {
		line, tail, more := strings.Cut(rest[offset:], "\n")
		if strings.TrimRight(line, " \t") == "---" {
			return rest[:offset], true
		}
		if !more {
			return "", false
		}
		offset = len(rest) - len(tail)
	}
}

// warnOnDuplicateNames reports skills sharing a frontmatter name. SEP-2640
// permits it and requires hosts to disambiguate, so this is a warning: it is
// legal, but rarely what an operator intended.
func warnOnDuplicateNames(ctx context.Context, entries []Entry) error {
	logger, err := util.LoggerFromContext(ctx)
	if err != nil {
		return fmt.Errorf("checking for duplicate skill names: %w", err)
	}
	byName := map[string][]string{}
	for _, e := range entries {
		if name, ok := e.Frontmatter["name"].(string); ok {
			byName[name] = append(byName[name], e.URI)
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if uris := byName[name]; len(uris) > 1 {
			logger.WarnContext(ctx, fmt.Sprintf("skills %s share the name %q; hosts must disambiguate them", strings.Join(uris, ", "), name))
		}
	}
	return nil
}
