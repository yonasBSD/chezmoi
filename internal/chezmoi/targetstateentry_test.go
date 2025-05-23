package chezmoi

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/muesli/combinator"
	vfs "github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"

	"github.com/twpayne/chezmoi/internal/chezmoitest"
)

func TestTargetStateEntryApply(t *testing.T) {
	targetStates := map[string]TargetStateEntry{
		"dir": &TargetStateDir{
			perm: fs.ModePerm &^ chezmoitest.Umask,
		},
		"file": &TargetStateFile{
			contentsFunc:       eagerNoErr([]byte("# contents of file")),
			contentsSHA256Func: eagerNoErr(sha256.Sum256([]byte("# contents of file"))),
			perm:               0o666 &^ chezmoitest.Umask,
		},
		"file_empty": &TargetStateFile{
			contentsFunc:       eagerZeroNoErr[[]byte](),
			contentsSHA256Func: eagerNoErr(sha256.Sum256(nil)),
			perm:               0o666 &^ chezmoitest.Umask,
			empty:              true,
		},
		"file_executable": &TargetStateFile{
			contentsFunc:       eagerNoErr([]byte("#!/bin/sh\n")),
			contentsSHA256Func: eagerNoErr(sha256.Sum256([]byte("#!/bin/sh\n"))),
			perm:               fs.ModePerm &^ chezmoitest.Umask,
		},
		"remove": &TargetStateRemove{},
		"symlink": &TargetStateSymlink{
			linknameFunc: eagerNoErr("target"),
		},
	}

	actualStates := map[string]map[string]any{
		"dir": {
			"/home/user/target": &vfst.Dir{Perm: fs.ModePerm},
		},
		"file": {
			"/home/user/target": "# contents of file",
		},
		"file_empty": {
			"/home/user/target": "",
		},
		"file_executable": {
			"/home/user/target": &vfst.File{
				Perm:     fs.ModePerm,
				Contents: []byte("!/bin/sh\n"),
			},
		},
		"remove": {
			"/home/user": &vfst.Dir{Perm: fs.ModePerm},
		},
		"symlink": {
			"/home/user": map[string]any{
				"symlink-target": "",
				"target":         &vfst.Symlink{Target: "symlink-target"},
			},
		},
		"symlink_broken": {
			"/home/user/target": &vfst.Symlink{Target: "symlink-target"},
		},
	}

	testData := struct {
		TargetStateKey        []string
		ActualDestDirStateKey []string
	}{
		TargetStateKey:        slices.Sorted(maps.Keys(targetStates)),
		ActualDestDirStateKey: slices.Sorted(maps.Keys(actualStates)),
	}
	var testCases []struct {
		TargetStateKey        string
		ActualDestDirStateKey string
	}
	assert.NoError(t, combinator.Generate(&testCases, testData))

	for _, tc := range testCases {
		name := fmt.Sprintf("target_%s_actual_%s", tc.TargetStateKey, tc.ActualDestDirStateKey)
		t.Run(name, func(t *testing.T) {
			targetState := targetStates[tc.TargetStateKey]
			actualState := actualStates[tc.ActualDestDirStateKey]

			chezmoitest.WithTestFS(t, actualState, func(fileSystem vfs.FS) {
				system := NewRealSystem(fileSystem)

				// Read the initial destination state entry from fileSystem.
				actualStateEntry, err := NewActualStateEntry(system, NewAbsPath("/home/user/target"), nil, nil)
				assert.NoError(t, err)

				// Apply the target state entry.
				_, err = targetState.Apply(system, nil, actualStateEntry)
				assert.NoError(t, err)

				// Verify that the actual state entry matches the desired
				// state.
				tests := vfst.TestPath("/home/user/target", targetStateTest(t, targetState)...)
				vfst.RunTests(t, fileSystem, "", tests)
			})
		})
	}
}

func targetStateTest(t *testing.T, ts TargetStateEntry) []vfst.PathTest {
	t.Helper()
	switch ts := ts.(type) {
	case *TargetStateRemove:
		return []vfst.PathTest{
			vfst.TestDoesNotExist(),
		}
	case *TargetStateDir:
		return []vfst.PathTest{
			vfst.TestIsDir(),
			vfst.TestModePerm(ts.perm &^ chezmoitest.Umask),
		}
	case *TargetStateFile:
		expectedContents, err := ts.Contents()
		assert.NoError(t, err)
		return []vfst.PathTest{
			vfst.TestModeIsRegular(),
			vfst.TestContents(expectedContents),
			vfst.TestModePerm(ts.perm &^ chezmoitest.Umask),
		}
	case *TargetStateScript:
		return nil
	case *TargetStateSymlink:
		expectedLinkname, err := ts.Linkname()
		assert.NoError(t, err)
		return []vfst.PathTest{
			vfst.TestModeType(fs.ModeSymlink),
			vfst.TestSymlinkTarget(expectedLinkname),
		}
	default:
		return nil
	}
}
