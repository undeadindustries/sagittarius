// Package storage owns the Sagittarius global home directory and the per-project
// slug registry. It mirrors the fork Storage + ProjectRegistry first-run
// footprint under ~/.sagittarius:
//
//   - EnsureGlobalHome creates ~/.sagittarius (0700) when missing.
//   - ProjectSlug maps an absolute project root to a short, human-readable slug,
//     persisting projects.json and tmp/<slug>/.project_root +
//     history/<slug>/.project_root ownership markers.
//
// Everything else under the home directory (settings.json, credentials,
// sessions, skills, extensions) is created lazily on first use, matching the
// fork behavior.
package storage
