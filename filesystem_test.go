package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/pyrra-dev/pyrra/slo"
)

func TestMatchObjectives(t *testing.T) {
	obj1 := slo.Objective{Labels: labels.FromStrings("foo", "bar")}
	obj2 := slo.Objective{Labels: labels.FromStrings("foo", "bar", "ying", "yang")}
	obj3 := slo.Objective{Labels: labels.FromStrings("foo", "bar", "yes", "no")}
	obj4 := slo.Objective{Labels: labels.FromStrings("foo", "baz")}

	objectives := Objectives{objectives: map[string]slo.Objective{}}
	objectives.Set(obj1)
	objectives.Set(obj2)
	objectives.Set(obj3)
	objectives.Set(obj4)

	matches := objectives.Match([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
	})
	require.Nil(t, matches)

	matches = objectives.Match([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
	})
	require.Len(t, matches, 3)
	require.Contains(t, matches, obj1)
	require.Contains(t, matches, obj2)
	require.Contains(t, matches, obj3)

	matches = objectives.Match([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "baz"),
	})
	require.Len(t, matches, 1)
	require.Contains(t, matches, obj4)

	matches = objectives.Match([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
		labels.MustNewMatcher(labels.MatchEqual, "ying", "yang"),
	})
	require.Len(t, matches, 1)
	require.Contains(t, matches, obj2)

	matches = objectives.Match([]*labels.Matcher{
		labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba."),
	})
	require.Len(t, matches, 4)
	require.Contains(t, matches, obj1)
	require.Contains(t, matches, obj2)
	require.Contains(t, matches, obj3)
	require.Contains(t, matches, obj4)
}

func TestFilesystemReadyHandler(t *testing.T) {
	dir := t.TempDir()

	// Empty glob: no files matched — handler should still report ready.
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	filesystemReadyHandler(filepath.Join(dir, "*.yaml"))(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// Existing readable file: ready.
	file := filepath.Join(dir, "slo.yaml")
	require.NoError(t, os.WriteFile(file, []byte("name: test"), 0o644))
	rr = httptest.NewRecorder()
	filesystemReadyHandler(filepath.Join(dir, "*.yaml"))(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	// File goes missing after glob: handler should return 503.
	require.NoError(t, os.Remove(file))
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing.yaml"), file))
	rr = httptest.NewRecorder()
	filesystemReadyHandler(filepath.Join(dir, "*.yaml"))(rr, req)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	// File exists but is not readable: handler should return 503.
	// Skipped when running as root (root bypasses permission bits).
	if os.Geteuid() != 0 {
		require.NoError(t, os.Remove(file))
		require.NoError(t, os.WriteFile(file, []byte("name: test"), 0o000))
		rr = httptest.NewRecorder()
		filesystemReadyHandler(filepath.Join(dir, "*.yaml"))(rr, req)
		require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	}

	// Malformed glob pattern: handler should return 503.
	rr = httptest.NewRecorder()
	filesystemReadyHandler("[")(rr, req)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)

	// Config directory disappears (e.g. configmap unmounted): handler should return 503.
	missingDir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(missingDir, 0o755))
	handler := filesystemReadyHandler(filepath.Join(missingDir, "*.yaml"))
	require.NoError(t, os.Remove(missingDir))
	rr = httptest.NewRecorder()
	handler(rr, req)
	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
}
