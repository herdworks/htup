package hyperproviders

import (
	"time"
)

// ============================ Package Variables ============================
var (
	LibVersion        = "v0.18.1"
	LibReleaseDate, _ = time.Parse(time.RFC3339, "2026-04-27T16:00:00-07:00") // Usage: LibReleaseDate.Format(time.RFC3339)
)
