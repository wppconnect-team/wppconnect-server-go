// Command openapi exports the same contract served at GET /openapi.json.
package main

import (
	"github.com/wppconnect-team/wppconnect-server-go/internal/httpapi"
	"os"
)

func main() {
	if _, err := os.Stdout.Write(httpapi.OpenAPISpec()); err != nil {
		panic(err)
	}
}
