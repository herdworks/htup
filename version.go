package hyperproviders

import (
	"time"
)

// ============================ Package Variables ============================
var (
	LibVersion        = "v0.19.1"
	LibReleaseDate, _ = time.Parse(time.RFC3339, "2026-05-19T09:25:04-07:00") // Usage: LibReleaseDate.Format(time.RFC3339)
)
