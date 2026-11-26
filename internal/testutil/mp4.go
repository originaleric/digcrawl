package testutil

import _ "embed"

// MP4 is a synthetic one-second blue video used by media lifecycle tests.
//
//go:embed testdata/blue.mp4
var MP4 string
