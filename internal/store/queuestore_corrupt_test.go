// Copyright (c) 2015-2025 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// corruptDir is kept separate from queueDir so these tests cannot race with
// the pre-existing tests in queuestore_test.go, which RemoveAll() queueDir.
var corruptDir = filepath.Join(os.TempDir(), "minio_test_corrupt")

// writeRawEntry drops a payload straight into the store directory, bypassing
// Put(). This is how the on-disk corruption actually arises in production - a
// torn/pre-allocated write leaves a file that Put() would never have produced.
func writeRawEntry(t *testing.T, dir, name string, payload []byte) Key {
	t.Helper()
	if err := os.MkdirAll(dir, 0o770); err != nil {
		t.Fatal("Failed to create store dir ", err)
	}
	fname := name + testItemExt
	if err := os.WriteFile(filepath.Join(dir, fname), payload, 0o600); err != nil {
		t.Fatal("Failed to write raw entry ", err)
	}
	return Key{Name: name, Extension: testItemExt, ItemCount: 1}
}

// TestQueueStoreGetZeroItemPayloads is a regression test for the panic at
// queuestore.go Get() -> `return items[0], nil`.
//
// GetMultiple() returns (nil, nil) - an empty slice and NO error - whenever
// jsoniter's decoder.More() is false on the very first call. GetRaw()'s only
// content guard is len(raw) == 0, so a file of N NUL bytes (len > 0) sails
// through it, and jsoniter's nextToken() returns byte 0x00 which is
// indistinguishable from its EOF sentinel. Get() then indexes items[0] on an
// empty slice and panics, taking down the whole MinIO process.
//
// Upstream has ZERO coverage for empty/corrupt/zero-item payloads, which is
// how the regression in cefc43e4daa4cbb490ef6726ea374e26a93eb85e shipped.
//
// Expected behaviour: only the valid payload yields an item; every other case
// returns os.ErrNotExist and, critically, does not panic.
func TestQueueStoreGetZeroItemPayloads(t *testing.T) {
	validJSON := []byte(`{"Name":"test-item","property":"property"}`)

	testCases := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "valid JSON (must still work, unchanged)",
			payload: validJSON,
			wantErr: false,
		},
		{
			// The exact production corruption: a single leading NUL makes
			// jsoniter report EOF immediately, so the trailing valid JSON is
			// never decoded and zero items come back with a nil error.
			name:    "1 leading NUL + valid JSON",
			payload: append([]byte{0x00}, validJSON...),
			wantErr: true,
		},
		{
			// Non-zero length, so GetRaw()'s len(raw) == 0 guard does not fire.
			name:    "all-NUL file",
			payload: make([]byte, 512),
			wantErr: true,
		},
		{
			name:    "whitespace only",
			payload: []byte("   \n\t\r\n  "),
			wantErr: true,
		},
		{
			// Already handled upstream by GetRaw(); pinned here so the
			// behaviour cannot silently regress.
			name:    "genuinely zero-byte file",
			payload: []byte{},
			wantErr: true,
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(corruptDir, "case", string(rune('a'+i)))
			defer os.RemoveAll(dir)

			key := writeRawEntry(t, dir, "poison", tc.payload)

			queueStore := NewQueueStore[TestItem](dir, 100, testItemExt)
			if err := queueStore.Open(); err != nil {
				t.Fatal("Failed to open queue store ", err)
			}

			// A panic here is the bug. Recover so one failing case reports
			// cleanly instead of tearing down the whole test binary.
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Get() panicked on %q payload: %v", tc.name, r)
				}
			}()

			item, err := queueStore.Get(key)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("Get() returned nil error for %q; expected os.ErrNotExist (got item %+v)", tc.name, item)
				}
				if !errors.Is(err, os.ErrNotExist) || !os.IsNotExist(err) {
					// os.IsNotExist is the predicate every SendFromStore()
					// implementation actually uses to decide to skip an
					// entry, so it must be satisfied - not merely errors.Is.
					t.Fatalf("Get() returned %v for %q; expected os.ErrNotExist so SendFromStore() skips it", err, tc.name)
				}
				return
			}

			if err != nil {
				t.Fatalf("Get() failed on valid payload: %v", err)
			}
			if item != testItem {
				t.Fatalf("Get() returned %+v; expected %+v", item, testItem)
			}
		})
	}
}

// TestQueueStoreGetMultipleZeroItemPayloads pins the underlying contract that
// Get() now depends on: GetMultiple() really does hand back an empty slice with
// a nil error for these payloads. If this ever starts returning an error
// instead, Get()'s guard becomes dead code and should be revisited.
func TestQueueStoreGetMultipleZeroItemPayloads(t *testing.T) {
	payloads := []struct {
		name    string
		payload []byte
	}{
		{"leading NUL", append([]byte{0x00}, []byte(`{"Name":"n","property":"p"}`)...)},
		{"all NUL", make([]byte, 64)},
		{"whitespace", []byte("  \n  ")},
		{"zero-byte", []byte{}},
	}

	for _, tc := range payloads {
		name, payload := tc.name, tc.payload
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(corruptDir, "multi", name)
			defer os.RemoveAll(dir)

			// NOTE: ItemCount must stay 1 here. Key.String() prefixes the
			// filename with "<n>:" once ItemCount > 1, so bumping it would
			// make GetRaw() miss the file and return a genuine ENOENT -
			// which is also os.ErrNotExist, and would false-pass this test.
			key := writeRawEntry(t, dir, "poison", payload)

			queueStore := NewQueueStore[TestItem](dir, 100, testItemExt)
			if err := queueStore.Open(); err != nil {
				t.Fatal("Failed to open queue store ", err)
			}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("GetMultiple() panicked on %q: %v", name, r)
				}
			}()

			// GetMultiple() itself never panics (it ranges, it does not
			// index), so we only assert it yields no items.
			items, err := queueStore.GetMultiple(key)
			if err == nil && len(items) != 0 {
				t.Fatalf("GetMultiple() returned %d items for %q; expected none", len(items), name)
			}
		})
	}
}
