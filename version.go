package hyperproviders

import (
	"time"
)

// ============================ Package Variables ============================
var (
	LibVersion        = "v0.19.0"
	LibReleaseDate, _ = time.Parse(time.RFC3339, "2026-04-30T13:00:00-07:00") // Usage: LibReleaseDate.Format(time.RFC3339)
)
