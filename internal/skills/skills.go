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

// Package skills implements the io.modelcontextprotocol/skills extension
// (SEP-2640). A skill is a directory of files served over the Resources
// primitive: SKILL.md at its root, addressed as skill://<skill-path>/SKILL.md,
// plus any supporting files sharing that prefix.
package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// DynamicMarker is what a skill publishes in place of a file list when its
// content is generated and cannot carry stable digests.
const DynamicMarker = "dynamic"

// Per-skill limits fixed by SEP-2640, both inclusive.
const (
	MaxRefs      = 512
	MaxTotalSize = 16 << 20 // 16 MiB, summed over every ref's Size
)

// ResourceRef is one file in a skill's manifest. Every field is required:
// size lets a host budget a skill before fetching anything, and a read whose
// length disagrees with it fails verification before any hashing.
type ResourceRef struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"` // "sha256:" followed by 64 lowercase hex characters
	Size   int64  `json:"size"`   // raw byte length of the content the digest covers
}

// Manifest is a skill's complete file list, or the marker saying it has none.
// SEP-2640 admits exactly those two forms and no third; the struct can hold
// neither and both, so Validate is what enforces the union.
type Manifest struct {
	// Refs lists every file of the skill, SKILL.md included. Ignored when
	// Dynamic is set.
	Refs []ResourceRef
	// Dynamic marks a skill whose content is generated, so no digest can be
	// published for it.
	Dynamic bool
}

// MarshalJSON emits the file list, or the string "dynamic".
func (m Manifest) MarshalJSON() ([]byte, error) {
	if m.Dynamic {
		return json.Marshal(DynamicMarker)
	}
	// Empty Refs means unpopulated, not a skill with no files. Emitting []
	// keeps the wire type stable; UnmarshalJSON will not read it back.
	if len(m.Refs) == 0 {
		return json.Marshal([]ResourceRef{})
	}
	return json.Marshal(m.Refs)
}

// UnmarshalJSON accepts either form and rejects anything else.
//
// Dispatching on the first byte parses once, where switching on a decoded any
// would build a throwaway tree and reparse. It also keeps null out: encoding/json
// reads null into both a string and a slice, leaving each at its zero value.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return fmt.Errorf("invalid skill manifest: no value")
	}

	switch data[0] {
	case '"':
		var marker string
		if err := json.Unmarshal(data, &marker); err != nil {
			return fmt.Errorf("invalid skill manifest: %w", err)
		}
		if marker != DynamicMarker {
			return fmt.Errorf("invalid skill manifest %q: the only permitted string is %q", truncate(marker), DynamicMarker)
		}
		m.Dynamic, m.Refs = true, nil
		return nil

	case '[':
		var refs []ResourceRef
		if err := json.Unmarshal(data, &refs); err != nil {
			return fmt.Errorf("invalid skill manifest: %w", err)
		}
		// A file list must be complete, and every skill has a SKILL.md.
		if len(refs) == 0 {
			return fmt.Errorf("invalid skill manifest: a file list names at least the skill's SKILL.md")
		}
		m.Dynamic, m.Refs = false, refs
		return nil

	default:
		return fmt.Errorf("invalid skill manifest: must be an array of {uri, digest, size} or the string %q", DynamicMarker)
	}
}

// Validate reports whether the manifest is one of SEP-2640's two forms, and
// whether a file list is one a conforming host will accept.
func (m Manifest) Validate() error {
	if m.Dynamic {
		if len(m.Refs) > 0 {
			return fmt.Errorf("invalid skill manifest: a dynamic skill publishes no file list, got %d refs", len(m.Refs))
		}
		// A dynamic entry offers nothing to count, so the limits do not apply.
		return nil
	}
	if len(m.Refs) == 0 {
		return fmt.Errorf("invalid skill manifest: a static skill lists at least its SKILL.md, or sets Dynamic")
	}
	if len(m.Refs) > MaxRefs {
		return fmt.Errorf("invalid skill manifest: %d refs exceeds the limit of %d", len(m.Refs), MaxRefs)
	}

	var total int64
	seen := make(map[string]struct{}, len(m.Refs))
	for i, r := range m.Refs {
		if r.URI == "" {
			return fmt.Errorf("invalid skill manifest: ref %d has no uri", i)
		}
		if _, dup := seen[r.URI]; dup {
			return fmt.Errorf("invalid skill manifest: %q is listed more than once", r.URI)
		}
		seen[r.URI] = struct{}{}

		if !validDigest(r.Digest) {
			return fmt.Errorf("invalid skill manifest: %q has digest %q, want sha256: followed by 64 lowercase hex characters", r.URI, r.Digest)
		}
		if r.Size < 0 {
			return fmt.Errorf("invalid skill manifest: %q has size %d, want a byte length", r.URI, r.Size)
		}
		// Subtraction, not addition: a size near math.MaxInt64 would wrap a
		// running total negative and pass.
		if r.Size > MaxTotalSize-total {
			return fmt.Errorf("invalid skill manifest: total size exceeds the limit of %d bytes", MaxTotalSize)
		}
		total += r.Size
	}
	return nil
}

// truncate bounds a value from the wire before it reaches an error message.
func truncate(s string) string {
	const max = 64
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// validDigest matches SEP-2640's sha256:{hex} form, {hex} being 64 lowercase
// hex characters. Uppercase is rejected, not folded: hosts compare as strings.
func validDigest(s string) bool {
	hex, ok := strings.CutPrefix(s, "sha256:")
	if !ok || len(hex) != 64 {
		return false
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Entry is one skill as skills/list and skills/get publish it.
type Entry struct {
	// URI addresses the skill's SKILL.md, not its root directory.
	URI string `json:"uri"`
	// Frontmatter is the SKILL.md YAML frontmatter verbatim. A host compares it
	// field by field against the file it fetches, so it must not be normalised.
	Frontmatter map[string]any `json:"frontmatter"`
	Resources   Manifest       `json:"resources"`
}

// UnmarshalJSON rejects an entry carrying no resources, which SEP-2640 calls
// invalid. Manifest cannot catch it: encoding/json never calls a custom
// unmarshaller for an absent key, leaving the field at the zero Manifest.
func (e *Entry) UnmarshalJSON(data []byte) error {
	// entry sheds this method so the decode does not recurse. RawMessage rather
	// than *Manifest, which would nil for both an absent key and a null.
	type entry Entry
	aux := struct {
		*entry
		Resources json.RawMessage `json:"resources"`
	}{entry: (*entry)(e)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return fmt.Errorf("invalid skill entry: %w", err)
	}
	if aux.Resources == nil {
		return fmt.Errorf("invalid skill entry %q: resources is required", e.URI)
	}
	return e.Resources.UnmarshalJSON(aux.Resources)
}

// Validate checks the rules relating a manifest to the skill's own identity.
// The URI rules are not tied to skill://, which SEP-2640 applies "regardless
// of scheme".
func (e Entry) Validate() error {
	root, ok := strings.CutSuffix(e.URI, "/SKILL.md")
	if !ok || root == "" {
		return fmt.Errorf("invalid skill entry %q: uri must address the skill's SKILL.md", truncate(e.URI))
	}
	// The last segment before SKILL.md is the skill name, which is what makes
	// the name recoverable from the URI without reading frontmatter.
	name := root[strings.LastIndex(root, "/")+1:]
	if name == "" {
		return fmt.Errorf("invalid skill entry %q: uri has no skill-path segment before SKILL.md", truncate(e.URI))
	}

	// Checked before the manifest: identity binds a dynamic skill too.
	fmName, err := requiredString(e.Frontmatter, "name")
	if err != nil {
		return fmt.Errorf("invalid skill entry %q: %w", truncate(e.URI), err)
	}
	if _, err := requiredString(e.Frontmatter, "description"); err != nil {
		return fmt.Errorf("invalid skill entry %q: %w", truncate(e.URI), err)
	}
	if fmName != name {
		return fmt.Errorf("invalid skill entry %q: frontmatter name %q does not match the uri's final skill-path segment %q", truncate(e.URI), truncate(fmName), name)
	}

	if err := e.Resources.Validate(); err != nil {
		return fmt.Errorf("skill %q: %w", truncate(e.URI), err)
	}
	if e.Resources.Dynamic {
		return nil
	}

	// A host resolves reads only to URIs the list carries, so a skill omitting
	// its own SKILL.md cannot be loaded at all.
	var listsItself bool
	for _, r := range e.Resources.Refs {
		if r.URI == e.URI {
			listsItself = true
			continue
		}
		if !strings.HasPrefix(r.URI, root+"/") {
			return fmt.Errorf("invalid skill entry %q: %q is not a file within the skill", truncate(e.URI), truncate(r.URI))
		}
	}
	if !listsItself {
		return fmt.Errorf("invalid skill entry %q: resources must list the skill's own SKILL.md", truncate(e.URI))
	}
	return nil
}

// requiredString reads a frontmatter field the Agent Skills specification
// requires. A nil map reports the field as absent.
func requiredString(fm map[string]any, key string) (string, error) {
	v, ok := fm[key]
	if !ok {
		return "", fmt.Errorf("frontmatter has no %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("frontmatter %s is %T, want a string", key, v)
	}
	if s == "" {
		return "", fmt.Errorf("frontmatter %s is empty", key)
	}
	return s, nil
}

// MarshalJSON keeps frontmatter an object when unpopulated: SEP-2640 requires
// the field and null is not an object. Validate is what rejects such an entry.
func (e Entry) MarshalJSON() ([]byte, error) {
	type entry Entry
	aux := entry(e)
	if aux.Frontmatter == nil {
		aux.Frontmatter = map[string]any{}
	}
	return json.Marshal(aux)
}
