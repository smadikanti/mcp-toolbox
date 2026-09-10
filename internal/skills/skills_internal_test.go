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
	"strings"
	"testing"
)

// TestValidSkillNameStandsAlone covers the bounds validSkillName has to enforce
// for itself. Entry.Validate reaches it only after requiredString has rejected
// an empty name, so the lower bound is untestable from outside the package.
func TestValidSkillNameStandsAlone(t *testing.T) {
	tcs := []struct {
		in      string
		wantErr string
	}{
		{in: "refunds"},
		{in: "a"},
		{in: strings.Repeat("a", 64)},
		{in: "", wantErr: "is 0 characters, want 1 to 64"},
		{in: strings.Repeat("a", 65), wantErr: "is 65 characters, want 1 to 64"},
	}

	for _, tc := range tcs {
		t.Run(tc.in, func(t *testing.T) {
			err := validSkillName(tc.in)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validSkillName(%q) = %v, want nil", tc.in, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validSkillName(%q) = nil, want error containing %q", tc.in, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("validSkillName(%q) = %v, want error containing %q", tc.in, err, tc.wantErr)
			}
		})
	}
}
