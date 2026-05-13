package cli

import "fmt"

// version is set via -ldflags "-X github.com/leonkozlowski/sparkmon/internal/cli.version=..."
var version = "dev"

// Version prints the build version.
func Version(args []string) int {
	fmt.Printf("sparkmon %s\n", version)
	return 0
}
