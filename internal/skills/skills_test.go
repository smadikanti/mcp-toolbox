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

package skills_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/googleapis/mcp-toolbox/internal/skills"
)

func TestManifestMarshalJSON(t *testing.T) {
	tcs := []struct {
		desc string
		in   skills.Manifest
		want string
	}{
		{
			desc: "static skill lists its files",
			in: skills.Manifest{Refs: []skills.ResourceRef{
				{URI: "skill://analytics-guide/SKILL.md", Digest: "sha256:a1b2", Size: 2314},
				{URI: "skill://analytics-guide/references/queries.md", Digest: "sha256:c3d4", Size: 962},
			}},
			want: `[{"uri":"skill://analytics-guide/SKILL.md","digest":"sha256:a1b2","size":2314},` +
				`{"uri":"skill://analytics-guide/references/queries.md","digest":"sha256:c3d4","size":962}]`,
		},
		{
			desc: "dynamic skill collapses to the marker",
			in:   skills.Manifest{Dynamic: true},
			want: `"dynamic"`,
		},
		{
			desc: "dynamic wins over any refs left set",
			in:   skills.Manifest{Dynamic: true, Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md"}}},
			want: `"dynamic"`,
		},
		{
			desc: "unpopulated static manifest stays an array",
			in:   skills.Manifest{},
			want: `[]`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal() = %v, want nil", err)
			}
			if string(got) != tc.want {
				t.Errorf("Marshal() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestManifestUnmarshalJSON(t *testing.T) {
	tcs := []struct {
		desc    string
		in      string
		want    skills.Manifest
		wantErr string
	}{
		{
			desc: "file list",
			in:   `[{"uri":"skill://x/SKILL.md","digest":"sha256:a1b2","size":10}]`,
			want: skills.Manifest{Refs: []skills.ResourceRef{
				{URI: "skill://x/SKILL.md", Digest: "sha256:a1b2", Size: 10},
			}},
		},
		{
			desc: "dynamic marker",
			in:   `"dynamic"`,
			want: skills.Manifest{Dynamic: true},
		},
		{
			desc:    "any other string is not a third form",
			in:      `"static"`,
			wantErr: `only permitted string is "dynamic"`,
		},
		{
			desc:    "an object is not a manifest",
			in:      `{"refs":[]}`,
			wantErr: "must be an array",
		},
		{
			// null unmarshals cleanly into both a string and a slice, so
			// without the type switch this is either reported as an
			// empty-string marker or silently accepted as an empty manifest.
			desc:    "null is not a manifest",
			in:      `null`,
			wantErr: "must be an array",
		},
		{
			desc:    "a number is not a manifest",
			in:      `42`,
			wantErr: "must be an array",
		},
		{
			// MarshalJSON emits [] for an unpopulated manifest; decoding
			// deliberately will not take it back.
			desc:    "an empty file list is not a skill with no files",
			in:      `[]`,
			wantErr: "names at least the skill's SKILL.md",
		},
		{
			desc:    "true is not a manifest",
			in:      `true`,
			wantErr: "must be an array",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			var got skills.Manifest
			err := json.Unmarshal([]byte(tc.in), &got)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Unmarshal() = nil, want error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Unmarshal() = %v, want error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal() = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unmarshal() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestManifestUnmarshalBoundsTheOffendingString keeps an error message from
// carrying a whole wire value into the logs.
func TestManifestUnmarshalBoundsTheOffendingString(t *testing.T) {
	var m skills.Manifest
	err := json.Unmarshal([]byte(`"`+strings.Repeat("a", 4000)+`"`), &m)
	if err == nil {
		t.Fatal("Unmarshal() = nil, want an error")
	}
	if got := len(err.Error()); got > 200 {
		t.Errorf("len(error) = %d, want the offending string truncated", got)
	}
	if !strings.Contains(err.Error(), "…") {
		t.Errorf("error = %v, want it to show truncation", err)
	}
}

// TestManifestUnmarshalEmptyInput covers the first-byte dispatch guard, which
// only a direct call can reach — but the method is exported.
func TestManifestUnmarshalEmptyInput(t *testing.T) {
	var m skills.Manifest
	if err := m.UnmarshalJSON([]byte("  ")); err == nil {
		t.Error("UnmarshalJSON() = nil, want an error")
	}
}

// Well-formed digests. Fixtures elsewhere use the spec's abbreviated
// placeholders, which Validate rejects by design.
const (
	digestA = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	digestB = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// manyRefs builds n distinctly-named well-formed refs of the given size, for
// exercising the per-skill limits.
func manyRefs(n int, size int64) []skills.ResourceRef {
	out := make([]skills.ResourceRef, n)
	for i := range out {
		out[i] = skills.ResourceRef{
			URI:    fmt.Sprintf("skill://x/f%d.md", i),
			Digest: digestA,
			Size:   size,
		}
	}
	return out
}

func TestManifestValidate(t *testing.T) {
	tcs := []struct {
		desc    string
		in      skills.Manifest
		wantErr string
	}{
		{
			desc: "static skill with one file",
			in:   skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: digestA, Size: 10}}},
		},
		{
			desc: "static skill with several files",
			in: skills.Manifest{Refs: []skills.ResourceRef{
				{URI: "skill://x/SKILL.md", Digest: digestA, Size: 10},
				{URI: "skill://x/refs/a.md", Digest: digestB, Size: 20},
			}},
		},
		{
			desc: "a zero-byte file is legal",
			in:   skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: digestA, Size: 0}}},
		},
		{
			desc: "dynamic skill",
			in:   skills.Manifest{Dynamic: true},
		},
		{
			desc:    "dynamic skill may not also list files",
			in:      skills.Manifest{Dynamic: true, Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: digestA}}},
			wantErr: "publishes no file list",
		},
		{
			// The zero value: neither form. Reachable from a struct literal
			// that forgets the field, so it is the state most worth catching.
			desc:    "neither form",
			in:      skills.Manifest{},
			wantErr: "lists at least its SKILL.md",
		},
		{
			desc:    "ref without a uri",
			in:      skills.Manifest{Refs: []skills.ResourceRef{{Digest: digestA, Size: 1}}},
			wantErr: "ref 0 has no uri",
		},
		{
			desc: "the same file listed twice",
			in: skills.Manifest{Refs: []skills.ResourceRef{
				{URI: "skill://x/SKILL.md", Digest: digestA, Size: 1},
				{URI: "skill://x/SKILL.md", Digest: digestB, Size: 2},
			}},
			wantErr: "listed more than once",
		},
		{
			desc:    "missing digest",
			in:      skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Size: 1}}},
			wantErr: "want sha256:",
		},
		{
			desc:    "wrong digest algorithm",
			in:      skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: "md5:0123456789abcdef0123456789abcdef", Size: 1}}},
			wantErr: "want sha256:",
		},
		{
			desc:    "digest too short",
			in:      skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: "sha256:a1b2", Size: 1}}},
			wantErr: "want sha256:",
		},
		{
			desc:    "uppercase digest is not folded",
			in:      skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: strings.ToUpper(digestA), Size: 1}}},
			wantErr: "want sha256:",
		},
		{
			desc:    "negative size",
			in:      skills.Manifest{Refs: []skills.ResourceRef{{URI: "skill://x/SKILL.md", Digest: digestA, Size: -1}}},
			wantErr: "want a byte length",
		},
		{
			desc: "at the ref limit",
			in:   skills.Manifest{Refs: manyRefs(skills.MaxRefs, 1)},
		},
		{
			desc:    "one ref past the limit",
			in:      skills.Manifest{Refs: manyRefs(skills.MaxRefs+1, 1)},
			wantErr: "exceeds the limit of 512",
		},
		{
			desc: "at the total size limit",
			in:   skills.Manifest{Refs: manyRefs(2, skills.MaxTotalSize/2)},
		},
		{
			desc: "one byte past the total size limit",
			in: skills.Manifest{Refs: append(
				manyRefs(2, skills.MaxTotalSize/2),
				skills.ResourceRef{URI: "skill://x/last.md", Digest: digestB, Size: 1},
			)},
			wantErr: "total size exceeds",
		},
		{
			// A naive running sum would wrap negative here and pass.
			desc: "a size that would overflow a running total",
			in: skills.Manifest{Refs: []skills.ResourceRef{
				{URI: "skill://x/SKILL.md", Digest: digestA, Size: skills.MaxTotalSize},
				{URI: "skill://x/huge.md", Digest: digestB, Size: math.MaxInt64},
			}},
			wantErr: "total size exceeds",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.in.Validate()

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestEntryMarshalJSON pins the shape SEP-2640 specifies for a skills/list
// entry: the URI addresses SKILL.md rather than the skill root, and frontmatter
// passes through verbatim because a host compares it field by field.
func TestEntryMarshalJSON(t *testing.T) {
	e := skills.Entry{
		URI: "skill://analytics-guide/SKILL.md",
		Frontmatter: map[string]any{
			"name":        "analytics-guide",
			"description": "Query and summarize the warehouse",
		},
		Resources: skills.Manifest{Refs: []skills.ResourceRef{
			{URI: "skill://analytics-guide/SKILL.md", Digest: "sha256:a1b2", Size: 2314},
		}},
	}

	got, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	want := `{"uri":"skill://analytics-guide/SKILL.md",` +
		`"frontmatter":{"description":"Query and summarize the warehouse","name":"analytics-guide"},` +
		`"resources":[{"uri":"skill://analytics-guide/SKILL.md","digest":"sha256:a1b2","size":2314}]}`
	if string(got) != want {
		t.Errorf("Marshal() =\n%s\nwant\n%s", got, want)
	}
}

func TestEntryUnmarshalJSON(t *testing.T) {
	tcs := []struct {
		desc    string
		in      string
		want    skills.Entry
		wantErr string
	}{
		{
			desc: "file list",
			in:   `{"uri":"skill://x/SKILL.md","frontmatter":{"name":"x"},"resources":[{"uri":"skill://x/SKILL.md","digest":"sha256:a1b2","size":10}]}`,
			want: skills.Entry{
				URI:         "skill://x/SKILL.md",
				Frontmatter: map[string]any{"name": "x"},
				Resources: skills.Manifest{Refs: []skills.ResourceRef{
					{URI: "skill://x/SKILL.md", Digest: "sha256:a1b2", Size: 10},
				}},
			},
		},
		{
			desc: "dynamic marker",
			in:   `{"uri":"skill://x/SKILL.md","frontmatter":{"name":"x"},"resources":"dynamic"}`,
			want: skills.Entry{
				URI:         "skill://x/SKILL.md",
				Frontmatter: map[string]any{"name": "x"},
				Resources:   skills.Manifest{Dynamic: true},
			},
		},
		{
			// Absent rather than malformed, so Manifest's UnmarshalJSON is
			// never reached.
			desc:    "no resources field at all",
			in:      `{"uri":"skill://x/SKILL.md","frontmatter":{"name":"x"}}`,
			wantErr: `"skill://x/SKILL.md": resources is required`,
		},
		{
			// Present but null still routes through Manifest, so it keeps the
			// error naming the two permitted forms.
			desc:    "null resources",
			in:      `{"uri":"skill://x/SKILL.md","frontmatter":{"name":"x"},"resources":null}`,
			wantErr: "must be an array",
		},
		{
			desc:    "empty resources",
			in:      `{"uri":"skill://x/SKILL.md","frontmatter":{"name":"x"},"resources":[]}`,
			wantErr: "names at least the skill's SKILL.md",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			var got skills.Entry
			err := json.Unmarshal([]byte(tc.in), &got)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Unmarshal() = nil, want error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("Unmarshal() = %v, want error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal() = %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unmarshal() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEntryValidate(t *testing.T) {
	// validEntry is the shape every case below perturbs in exactly one way.
	validEntry := func() skills.Entry {
		return skills.Entry{
			URI:         "skill://acme/billing/refunds/SKILL.md",
			Frontmatter: map[string]any{"name": "refunds", "description": "Process refunds"},
			Resources: skills.Manifest{Refs: []skills.ResourceRef{
				{URI: "skill://acme/billing/refunds/SKILL.md", Digest: digestA, Size: 10},
				{URI: "skill://acme/billing/refunds/examples/email.md", Digest: digestB, Size: 20},
			}},
		}
	}

	tcs := []struct {
		desc    string
		mutate  func(*skills.Entry)
		wantErr string
	}{
		{
			desc:   "a well-formed nested skill",
			mutate: func(*skills.Entry) {},
		},
		{
			desc: "a well-formed single-segment skill",
			mutate: func(e *skills.Entry) {
				e.URI = "skill://git-workflow/SKILL.md"
				e.Frontmatter = map[string]any{"name": "git-workflow", "description": "Git conventions"}
				e.Resources = skills.Manifest{Refs: []skills.ResourceRef{
					{URI: "skill://git-workflow/SKILL.md", Digest: digestA, Size: 10},
				}}
			},
		},
		{
			// SEP-2640 applies the URI structure "regardless of scheme".
			desc: "a non-skill:// scheme is still valid",
			mutate: func(e *skills.Entry) {
				e.URI = "github://owner/repo/skills/refunds/SKILL.md"
				e.Resources = skills.Manifest{Refs: []skills.ResourceRef{
					{URI: "github://owner/repo/skills/refunds/SKILL.md", Digest: digestA, Size: 10},
				}}
			},
		},
		{
			desc: "a dynamic skill needs no file list",
			mutate: func(e *skills.Entry) {
				e.Resources = skills.Manifest{Dynamic: true}
			},
		},
		{
			desc:    "uri must address SKILL.md",
			mutate:  func(e *skills.Entry) { e.URI = "skill://acme/billing/refunds" },
			wantErr: "uri must address the skill's SKILL.md",
		},
		{
			desc:    "uri needs a skill-path segment",
			mutate:  func(e *skills.Entry) { e.URI = "/SKILL.md" },
			wantErr: "uri must address the skill's SKILL.md",
		},
		{
			desc:    "an empty path segment is not a skill name",
			mutate:  func(e *skills.Entry) { e.URI = "skill://acme/billing//SKILL.md" },
			wantErr: "no skill-path segment before SKILL.md",
		},
		{
			desc:    "frontmatter is required",
			mutate:  func(e *skills.Entry) { e.Frontmatter = nil },
			wantErr: "frontmatter has no name",
		},
		{
			desc:    "frontmatter needs a description",
			mutate:  func(e *skills.Entry) { e.Frontmatter = map[string]any{"name": "refunds"} },
			wantErr: "frontmatter has no description",
		},
		{
			desc: "a non-string name is malformed",
			mutate: func(e *skills.Entry) {
				e.Frontmatter = map[string]any{"name": 42, "description": "Process refunds"}
			},
			wantErr: "frontmatter name is int, want a string",
		},
		{
			desc: "an empty description is not a description",
			mutate: func(e *skills.Entry) {
				e.Frontmatter = map[string]any{"name": "refunds", "description": ""}
			},
			wantErr: "frontmatter description is empty",
		},
		{
			// The name must be recoverable from the uri alone, which only
			// holds while the two agree.
			desc: "frontmatter name disagreeing with the uri",
			mutate: func(e *skills.Entry) {
				e.Frontmatter = map[string]any{"name": "returns", "description": "Process refunds"}
			},
			wantErr: `frontmatter name "returns" does not match`,
		},
		{
			desc: "a dynamic skill still has its uri and name checked",
			mutate: func(e *skills.Entry) {
				e.Resources = skills.Manifest{Dynamic: true}
				e.Frontmatter = map[string]any{"name": "returns", "description": "Process refunds"}
			},
			wantErr: "does not match",
		},
		{
			desc: "resources must list the skill's own SKILL.md",
			mutate: func(e *skills.Entry) {
				e.Resources = skills.Manifest{Refs: []skills.ResourceRef{
					{URI: "skill://acme/billing/refunds/examples/email.md", Digest: digestB, Size: 20},
				}}
			},
			wantErr: "must list the skill's own SKILL.md",
		},
		{
			desc: "a file outside the skill directory",
			mutate: func(e *skills.Entry) {
				e.Resources.Refs = append(e.Resources.Refs,
					skills.ResourceRef{URI: "skill://acme/billing/invoices/secret.md", Digest: digestA, Size: 5})
			},
			wantErr: "is not a file within the skill",
		},
		{
			// A sibling whose path merely starts with the skill's name is not
			// inside it; the prefix test has to include the separator.
			desc: "a sibling sharing the name prefix",
			mutate: func(e *skills.Entry) {
				e.Resources.Refs = append(e.Resources.Refs,
					skills.ResourceRef{URI: "skill://acme/billing/refunds-archive/old.md", Digest: digestA, Size: 5})
			},
			wantErr: "is not a file within the skill",
		},
		{
			desc: "manifest errors propagate",
			mutate: func(e *skills.Entry) {
				e.Resources.Refs[1].Digest = "sha256:nope"
			},
			wantErr: "want sha256:",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			e := validEntry()
			tc.mutate(&e)
			err := e.Validate()

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestEntryMarshalsFrontmatterAsObject(t *testing.T) {
	got, err := json.Marshal(skills.Entry{})
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	if strings.Contains(string(got), `"frontmatter":null`) {
		t.Errorf("Marshal() = %s, want frontmatter to stay an object", got)
	}
}

func TestEntryRoundTripsDynamic(t *testing.T) {
	in := skills.Entry{
		URI:         "skill://drafting/SKILL.md",
		Frontmatter: map[string]any{"name": "drafting"},
		Resources:   skills.Manifest{Dynamic: true},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	if !strings.Contains(string(data), `"resources":"dynamic"`) {
		t.Fatalf("Marshal() = %s, want resources to be the dynamic marker", data)
	}

	var got skills.Entry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() = %v, want nil", err)
	}
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}

// TestEntryMatchesSEPExample round-trips a fixture derived from SEP-2640's
// "Retrieval via skills/get" example, so a renamed or dropped field fails here.
// It pins round-trip stability over the spec's field set, not byte fidelity to
// the document: frontmatter keys are in Go's sorted order because encoding/json
// sorts map keys, and the list is three of the example's six entries. The
// elided placeholder digests make it an invalid manifest by design.
func TestEntryMatchesSEPExample(t *testing.T) {
	const sepExample = `{"uri":"skill://pdf-processing/SKILL.md",` +
		`"frontmatter":{"description":"Extract, fill, and assemble PDF documents","metadata":{"version":"2.1.0"},"name":"pdf-processing"},` +
		`"resources":[` +
		`{"uri":"skill://pdf-processing/SKILL.md","digest":"sha256:d5e6f7a8...","size":5120},` +
		`{"uri":"skill://pdf-processing/references/FORMS.md","digest":"sha256:e6f7a8b9...","size":18433},` +
		`{"uri":"skill://pdf-processing/scripts/extract.py","digest":"sha256:f7a8b9c0...","size":4096}]}`

	var e skills.Entry
	if err := json.Unmarshal([]byte(sepExample), &e); err != nil {
		t.Fatalf("Unmarshal() = %v, want nil", err)
	}
	if e.URI != "skill://pdf-processing/SKILL.md" {
		t.Errorf("URI = %q, want the SKILL.md URI", e.URI)
	}
	if got := len(e.Resources.Refs); got != 3 {
		t.Errorf("len(Refs) = %d, want 3", got)
	}
	if e.Resources.Dynamic {
		t.Error("Dynamic = true, want false for a manifest carrying a file list")
	}

	got, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() = %v, want nil", err)
	}
	if string(got) != sepExample {
		t.Errorf("round trip differs from the SEP example:\n got %s\nwant %s", got, sepExample)
	}
}
