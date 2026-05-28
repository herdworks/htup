package hyperproviders

import (
	"time"
)

// ============================ Package Variables ============================
var (
	LibVersion        = "v0.23.0"
	LibReleaseDate, _ = time.Parse(time.RFC3339, "2026-05-28T15:00:00-07:00") // Usage: LibReleaseDate.Format(time.RFC3339)
)
